package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"math"
	"sort"
)

func scoreFunctions(functions []sourceFunction, profile *coverageProfile, threshold float64) []functionResult {
	results := make([]functionResult, 0, len(functions))
	for _, function := range functions {
		coverage, available := functionCoverage(function, profile)
		branches := functionBranches(function, profile)
		result := functionResult{
			Path:              function.Path,
			Name:              function.Name,
			Line:              function.StartLine,
			Complexity:        function.Complexity,
			Coverage:          coverage,
			CoverageAvailable: available,
			CRAP:              crapScore(function.Complexity, coverage),
			Branches:          branches,
		}
		result.Crappy = result.CRAP >= threshold
		for _, branch := range branches {
			switch branch.Status {
			case branchCovered:
				result.CoveredBranches++
			case branchUncovered:
				result.UncoveredBranches++
			case branchUnknown:
				result.UnknownBranches++
			}
		}
		results = append(results, result)
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].CRAP != results[j].CRAP {
			return results[i].CRAP > results[j].CRAP
		}
		if results[i].Path != results[j].Path {
			return results[i].Path < results[j].Path
		}
		return results[i].Line < results[j].Line
	})
	return results
}

func crapScore(complexity int, coverage float64) float64 {
	uncovered := 1 - coverage
	return math.Pow(float64(complexity), 2)*math.Pow(uncovered, 3) + float64(complexity)
}

func functionCoverage(function sourceFunction, profile *coverageProfile) (float64, bool) {
	if profile == nil || !profile.hasFile(function.Path) {
		return 0, false
	}
	var total, covered int
	for _, block := range profile.Files[function.Path] {
		if !positionInFunction(block.Start, function) || inExcludedSpan(block.Start, function.Excluded) {
			continue
		}
		total += block.Statements
		if block.Count > 0 {
			covered += block.Statements
		}
	}
	if total == 0 {
		return 1, true
	}
	return float64(covered) / float64(total), true
}

func positionInFunction(position sourcePosition, function sourceFunction) bool {
	bodyStart := function.FileSet.Position(function.Decl.Body.Lbrace)
	bodyEnd := function.FileSet.Position(function.Decl.Body.Rbrace)
	return comparePosition(position, sourcePosition{Line: bodyStart.Line, Column: bodyStart.Column}) > 0 &&
		comparePosition(position, sourcePosition{Line: bodyEnd.Line, Column: bodyEnd.Column}) < 0
}

func comparePosition(left, right sourcePosition) int {
	if left.Line < right.Line || left.Line == right.Line && left.Column < right.Column {
		return -1
	}
	if left == right {
		return 0
	}
	return 1
}

func inExcludedSpan(position sourcePosition, spans []sourceSpan) bool {
	for _, span := range spans {
		if comparePosition(position, span.Start) >= 0 && comparePosition(position, span.End) <= 0 {
			return true
		}
	}
	return false
}

func functionBranches(function sourceFunction, profile *coverageProfile) []branchResult {
	if profile != nil {
		var explicit []branchResult
		for _, branch := range profile.Branches[function.Path] {
			if branch.Line < function.StartLine || branch.Line > function.EndLine {
				continue
			}
			status := branchUncovered
			if branch.Taken > 0 {
				status = branchCovered
			}
			explicit = append(explicit, branchResult{
				Line: branch.Line, Label: fmt.Sprintf("LCOV block %s branch %s", branch.Block, branch.Branch), Status: status,
			})
		}
		if len(explicit) > 0 {
			sortBranches(explicit)
			return explicit
		}
	}

	var branches []branchResult
	fset := function.FileSet
	ast.Inspect(function.Decl.Body, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.FuncLit:
			return false
		case *ast.IfStmt:
			branches = append(branches, armBranch(fset, profile, function.Path, n.If, "if true", n.Body))
			if n.Else == nil {
				branches = append(branches, inferredIfFalseBranch(fset, profile, function.Path, n))
			} else {
				branches = append(branches, armBranch(fset, profile, function.Path, n.If, "if false", n.Else))
			}
		case *ast.ForStmt:
			branches = append(branches, armBranch(fset, profile, function.Path, n.For, "loop body", n.Body))
			branches = append(branches, unknownBranch(fset, n.For, "loop exit", "Go coverage does not instrument the loop condition edge"))
		case *ast.RangeStmt:
			branches = append(branches, armBranch(fset, profile, function.Path, n.For, "range body", n.Body))
			branches = append(branches, unknownBranch(fset, n.For, "range exhausted or empty", "Go coverage does not instrument the range exit edge"))
		case *ast.SwitchStmt:
			if !hasDefaultClause(n.Body) {
				branches = append(branches, unknownBranch(fset, n.Switch, "switch no match", "Go coverage has no distinct block for an unmatched switch"))
			}
		case *ast.TypeSwitchStmt:
			if !hasDefaultClause(n.Body) {
				branches = append(branches, unknownBranch(fset, n.Switch, "type switch no match", "Go coverage has no distinct block for an unmatched switch"))
			}
		case *ast.CaseClause:
			label := "case"
			if n.List == nil {
				label = "default"
			}
			branches = append(branches, statementArmBranch(fset, profile, function.Path, n.Case, label, n.Body, n))
		case *ast.CommClause:
			label := "select case"
			if n.Comm == nil {
				label = "select default"
			}
			branches = append(branches, statementArmBranch(fset, profile, function.Path, n.Case, label, n.Body, n))
		case *ast.BinaryExpr:
			if n.Op == token.LAND || n.Op == token.LOR {
				line := fset.Position(n.OpPos).Line
				operator := n.Op.String()
				reason := "Go coverage deliberately does not instrument short-circuit operands separately"
				branches = append(branches,
					branchResult{Line: line, Label: operator + " right operand", Status: branchUnknown, Reason: reason},
					branchResult{Line: line, Label: operator + " short-circuit", Status: branchUnknown, Reason: reason},
				)
			}
		}
		return true
	})
	sortBranches(branches)
	return branches
}

func armBranch(fset *token.FileSet, profile *coverageProfile, path string, position token.Pos, label string, node ast.Node) branchResult {
	line := fset.Position(position).Line
	status, reason := coverageForNode(fset, profile, path, node)
	return branchResult{Line: line, Label: label, Status: status, Reason: reason}
}

func statementArmBranch(fset *token.FileSet, profile *coverageProfile, path string, position token.Pos, label string, statements []ast.Stmt, fallback ast.Node) branchResult {
	if len(statements) > 0 {
		return armBranch(fset, profile, path, position, label, statements[0])
	}
	line := fset.Position(position).Line
	status, reason := coverageForNode(fset, profile, path, fallback)
	if status == branchUnknown && reason == "no coverage block maps to this branch" {
		reason = "empty branch has no executable statement to instrument"
	}
	return branchResult{Line: line, Label: label, Status: status, Reason: reason}
}

func unknownBranch(fset *token.FileSet, position token.Pos, label, reason string) branchResult {
	return branchResult{Line: fset.Position(position).Line, Label: label, Status: branchUnknown, Reason: reason}
}

func inferredIfFalseBranch(fset *token.FileSet, profile *coverageProfile, path string, statement *ast.IfStmt) branchResult {
	line := fset.Position(statement.If).Line
	result := branchResult{Line: line, Label: "if false"}
	if profile == nil || !profile.hasFile(path) {
		result.Status = branchUnknown
		result.Reason = "file is absent from the coverage profile"
		return result
	}
	if !profile.CanCompareCounts {
		result.Status = branchUnknown
		result.Reason = "set-mode coverage cannot distinguish repeated true and false outcomes"
		return result
	}
	if profile.Format == "lcov" && len(statement.Body.List) > 0 && fset.Position(statement.Body.List[0].Pos()).Line == line {
		result.Status = branchUnknown
		result.Reason = "line-only LCOV cannot distinguish same-line branch counters"
		return result
	}
	decisionCount, decisionFound := coverageCountForNode(fset, profile, path, statement)
	trueCount, trueFound := coverageCountForNode(fset, profile, path, statement.Body)
	if !decisionFound || !trueFound || decisionCount < trueCount {
		result.Status = branchUnknown
		result.Reason = "Go coverage has no distinct block for fallthrough"
		return result
	}
	if decisionCount > trueCount {
		result.Status = branchCovered
	} else {
		result.Status = branchUncovered
	}
	return result
}

func coverageForNode(fset *token.FileSet, profile *coverageProfile, path string, node ast.Node) (branchStatus, string) {
	if profile == nil || !profile.hasFile(path) {
		return branchUnknown, "file is absent from the coverage profile"
	}
	if block, ok := node.(*ast.BlockStmt); ok && len(block.List) > 0 {
		node = block.List[0]
	}
	startToken := fset.Position(node.Pos())
	endToken := fset.Position(node.End())
	start := sourcePosition{Line: startToken.Line, Column: startToken.Column}
	end := sourcePosition{Line: endToken.Line, Column: endToken.Column}
	found := false
	covered := false
	for _, block := range profile.Files[path] {
		containsStart := comparePosition(block.Start, start) <= 0 && comparePosition(start, block.End) <= 0
		startsInside := comparePosition(block.Start, start) >= 0 && comparePosition(block.Start, end) <= 0
		if !containsStart && !startsInside {
			continue
		}
		found = true
		if block.Count > 0 {
			covered = true
		}
	}
	if !found {
		return branchUnknown, "no coverage block maps to this branch"
	}
	if covered {
		return branchCovered, ""
	}
	return branchUncovered, ""
}

func coverageCountForNode(fset *token.FileSet, profile *coverageProfile, path string, node ast.Node) (int64, bool) {
	if block, ok := node.(*ast.BlockStmt); ok && len(block.List) > 0 {
		node = block.List[0]
	}
	positionToken := fset.Position(node.Pos())
	position := sourcePosition{Line: positionToken.Line, Column: positionToken.Column}
	var count int64
	found := false
	for _, block := range profile.Files[path] {
		if comparePosition(block.Start, position) <= 0 && comparePosition(position, block.End) <= 0 {
			if !found || block.Count > count {
				count = block.Count
			}
			found = true
		}
	}
	return count, found
}

func hasDefaultClause(body *ast.BlockStmt) bool {
	for _, statement := range body.List {
		clause, ok := statement.(*ast.CaseClause)
		if ok && clause.List == nil {
			return true
		}
	}
	return false
}

func sortBranches(branches []branchResult) {
	sort.SliceStable(branches, func(i, j int) bool {
		if branches[i].Line != branches[j].Line {
			return branches[i].Line < branches[j].Line
		}
		return branches[i].Label < branches[j].Label
	})
}
