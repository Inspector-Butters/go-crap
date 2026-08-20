package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func discoverFunctions(repo string, changes changeSet, includeTests, includeGenerated bool) ([]sourceFunction, []string, error) {
	paths := make([]string, 0, len(changes))
	for path := range changes {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	var functions []sourceFunction
	var skipped []string
	for _, path := range paths {
		if !strings.HasSuffix(path, ".go") {
			continue
		}
		if !includeTests && strings.HasSuffix(path, "_test.go") {
			continue
		}
		fullPath := filepath.Join(repo, filepath.FromSlash(path))
		source, err := os.ReadFile(fullPath)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, nil, fmt.Errorf("read %s: %w", path, err)
		}
		if !includeGenerated && isGenerated(source) {
			skipped = append(skipped, path+" (generated)")
			continue
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, fullPath, source, parser.ParseComments)
		if err != nil {
			return nil, nil, fmt.Errorf("parse %s: %w", path, err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			start := fset.Position(fn.Pos()).Line
			end := fset.Position(fn.End()).Line
			if !intersectsChangedLines(start, end, changes[path]) {
				continue
			}
			functions = append(functions, sourceFunction{
				Path:       path,
				Name:       functionName(fset, fn),
				StartLine:  start,
				EndLine:    end,
				Complexity: cyclomaticComplexity(fn.Body),
				Decl:       fn,
				FileSet:    fset,
				Excluded:   nestedFunctionSpans(fset, fn.Body),
			})
		}
	}
	return functions, skipped, nil
}

func isGenerated(source []byte) bool {
	const marker = "Code generated "
	const suffix = " DO NOT EDIT."
	for _, line := range bytes.Split(source, []byte{'\n'}) {
		trimmed := strings.TrimSpace(string(line))
		if strings.HasPrefix(trimmed, "package ") {
			break
		}
		if strings.HasPrefix(trimmed, "// "+marker) && strings.HasSuffix(trimmed, suffix) {
			return true
		}
	}
	return false
}

func intersectsChangedLines(start, end int, ranges []lineRange) bool {
	for _, r := range ranges {
		if r.Start <= end && r.End >= start {
			return true
		}
	}
	return false
}

func functionName(fset *token.FileSet, fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return fn.Name.Name
	}
	var receiver bytes.Buffer
	if err := format.Node(&receiver, fset, fn.Recv.List[0].Type); err != nil {
		return fn.Name.Name
	}
	return "(" + receiver.String() + ")." + fn.Name.Name
}

func cyclomaticComplexity(body *ast.BlockStmt) int {
	complexity := 1
	ast.Inspect(body, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.FuncLit:
			return false
		case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt:
			complexity++
		case *ast.CaseClause:
			if n.List != nil {
				complexity++
			}
		case *ast.CommClause:
			if n.Comm != nil {
				complexity++
			}
		case *ast.BinaryExpr:
			if n.Op == token.LAND || n.Op == token.LOR {
				complexity++
			}
		}
		return true
	})
	return complexity
}

func nestedFunctionSpans(fset *token.FileSet, body *ast.BlockStmt) []sourceSpan {
	var spans []sourceSpan
	ast.Inspect(body, func(node ast.Node) bool {
		literal, ok := node.(*ast.FuncLit)
		if !ok {
			return true
		}
		start := fset.Position(literal.Pos())
		end := fset.Position(literal.End())
		spans = append(spans, sourceSpan{
			Start: sourcePosition{Line: start.Line, Column: start.Column},
			End:   sourcePosition{Line: end.Line, Column: end.Column},
		})
		return false
	})
	return spans
}
