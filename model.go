package main

import (
	"go/ast"
	"go/token"
)

type lineRange struct {
	Start int
	End   int
}

type changeSet map[string][]lineRange

type sourceFunction struct {
	Path       string
	Name       string
	StartLine  int
	EndLine    int
	Complexity int
	Decl       *ast.FuncDecl
	FileSet    *token.FileSet
	Excluded   []sourceSpan
}

type sourceSpan struct {
	Start sourcePosition
	End   sourcePosition
}

type sourcePosition struct {
	Line   int
	Column int
}

type coverageBlock struct {
	Start      sourcePosition
	End        sourcePosition
	Statements int
	Count      int64
}

type explicitBranch struct {
	Line   int
	Block  string
	Branch string
	Taken  int64
}

type coverageProfile struct {
	Format           string
	CanCompareCounts bool
	Files            map[string][]coverageBlock
	Branches         map[string][]explicitBranch
}

type branchStatus string

const (
	branchCovered   branchStatus = "covered"
	branchUncovered branchStatus = "uncovered"
	branchUnknown   branchStatus = "unknown"
)

type branchResult struct {
	Line   int          `json:"line"`
	Label  string       `json:"label"`
	Status branchStatus `json:"status"`
	Reason string       `json:"reason,omitempty"`
}

type functionResult struct {
	Path              string         `json:"path"`
	Name              string         `json:"name"`
	Line              int            `json:"line"`
	Complexity        int            `json:"complexity"`
	Coverage          float64        `json:"coverage"`
	CoverageAvailable bool           `json:"coverage_available"`
	CRAP              float64        `json:"crap"`
	Crappy            bool           `json:"crappy"`
	CoveredBranches   int            `json:"covered_branches"`
	UncoveredBranches int            `json:"uncovered_branches"`
	UnknownBranches   int            `json:"unknown_branches"`
	Branches          []branchResult `json:"branches"`
}

type report struct {
	VCS                string           `json:"vcs"`
	Base               string           `json:"base"`
	CoverageFormat     string           `json:"coverage_format"`
	Threshold          float64          `json:"threshold"`
	Functions          []functionResult `json:"functions"`
	AverageCRAP        float64          `json:"average_crap"`
	MaximumCRAP        float64          `json:"maximum_crap"`
	CrappyFunctions    int              `json:"crappy_functions"`
	CrappyFunctionRate float64          `json:"crappy_function_rate"`
}
