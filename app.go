package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
)

var version = "0.3.1"

type stringList []string

func (values *stringList) String() string { return strings.Join(*values, " ") }
func (values *stringList) Set(value string) error {
	*values = append(*values, value)
	return nil
}

type options struct {
	repo             string
	vcs              string
	base             string
	profile          string
	runTests         bool
	includeTests     bool
	includeGenerated bool
	threshold        float64
	failThreshold    bool
	json             bool
	allBranches      bool
	showVersion      bool
	testArgs         stringList
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	runUpdateCheck(ctx, stderr)

	var options options
	flags := flag.NewFlagSet("go-crap", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.StringVar(&options.repo, "repo", ".", "Git or jj workspace to analyze")
	flags.StringVar(&options.vcs, "vcs", "auto", "version-control backend: auto, git, or jj")
	flags.StringVar(&options.base, "base", "HEAD", "Git revision or jj revset to use as the comparison base")
	flags.StringVar(&options.profile, "coverprofile", "", "existing Go or LCOV coverage profile (skips automatic tests)")
	flags.BoolVar(&options.runTests, "run-tests", true, "run go test for affected packages when no profile is supplied")
	flags.BoolVar(&options.includeTests, "include-tests", false, "analyze functions in *_test.go files")
	flags.BoolVar(&options.includeGenerated, "include-generated", false, "analyze generated Go files")
	flags.Float64Var(&options.threshold, "threshold", 30, "score at which a function is considered CRAPpy")
	flags.BoolVar(&options.failThreshold, "fail", false, "exit with status 1 when a function reaches the threshold")
	flags.BoolVar(&options.json, "json", false, "write JSON instead of the text report")
	flags.BoolVar(&options.allBranches, "all-branches", false, "list covered branches as well as uncovered and unknown branches")
	flags.BoolVar(&options.showVersion, "version", false, "print the version")
	flags.Var(&options.testArgs, "test-arg", "extra argument for go test (repeatable)")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "usage: go-crap [options]")
		fmt.Fprintln(stderr, "Analyze changed Go functions using cyclomatic complexity and test coverage.")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if len(flags.Args()) > 0 {
		fmt.Fprintf(stderr, "go-crap: unexpected arguments: %s\n", strings.Join(flags.Args(), " "))
		return 2
	}
	if options.showVersion {
		fmt.Fprintf(stdout, "go-crap %s\n", version)
		return 0
	}
	if options.threshold <= 0 {
		fmt.Fprintln(stderr, "go-crap: threshold must be greater than zero")
		return 2
	}

	repository, err := resolveRepository(ctx, options.repo, vcsBackend(options.vcs))
	if err != nil {
		fmt.Fprintf(stderr, "go-crap: %v\n", err)
		return 2
	}
	changes, err := repositoryChanges(ctx, repository, options.base)
	if err != nil {
		fmt.Fprintf(stderr, "go-crap: %v\n", err)
		return 2
	}
	functions, skipped, err := discoverFunctions(repository.Root, changes, options.includeTests, options.includeGenerated)
	if err != nil {
		fmt.Fprintf(stderr, "go-crap: %v\n", err)
		return 2
	}
	for _, path := range skipped {
		fmt.Fprintf(stderr, "go-crap: skipped %s\n", path)
	}

	profilePath := options.profile
	var temporaryProfile string
	if len(functions) > 0 && profilePath == "" && options.runTests {
		file, createErr := os.CreateTemp("", "go-crap-coverage-*.out")
		if createErr != nil {
			fmt.Fprintf(stderr, "go-crap: create temporary coverage profile: %v\n", createErr)
			return 2
		}
		temporaryProfile = file.Name()
		_ = file.Close()
		defer os.Remove(temporaryProfile)
		if err := runAffectedTests(ctx, repository.Root, functions, temporaryProfile, options.testArgs, stderr); err != nil {
			fmt.Fprintf(stderr, "go-crap: %v\n", err)
			return 2
		}
		profilePath = temporaryProfile
	}

	var profile *coverageProfile
	if profilePath != "" && len(functions) > 0 {
		known := make([]string, 0, len(functions))
		seen := make(map[string]bool)
		for _, function := range functions {
			if !seen[function.Path] {
				known = append(known, function.Path)
				seen[function.Path] = true
			}
		}
		profile, err = loadCoverageProfile(profilePath, repository.Root, known)
		if err != nil {
			fmt.Fprintf(stderr, "go-crap: %v\n", err)
			return 2
		}
	}
	results := scoreFunctions(functions, profile, options.threshold)
	report := buildReport(string(repository.Backend), options.base, options.threshold, profile, results)
	if options.json {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(report); err != nil {
			fmt.Fprintf(stderr, "go-crap: write report: %v\n", err)
			return 2
		}
	} else {
		writeTextReport(stdout, report, options.allBranches)
	}
	if options.failThreshold && report.CrappyFunctions > 0 {
		return 1
	}
	return 0
}

func runAffectedTests(ctx context.Context, repo string, functions []sourceFunction, profile string, extraArgs []string, output io.Writer) error {
	packageSet := make(map[string]bool)
	for _, function := range functions {
		dir := filepath.ToSlash(filepath.Dir(function.Path))
		if dir == "." {
			packageSet["."] = true
		} else {
			packageSet["./"+dir] = true
		}
	}
	packages := make([]string, 0, len(packageSet))
	for pkg := range packageSet {
		packages = append(packages, pkg)
	}
	sort.Strings(packages)
	args := []string{"test", "-covermode=count", "-coverprofile=" + profile}
	args = append(args, extraArgs...)
	args = append(args, packages...)
	fmt.Fprintf(output, "go-crap: running go %s\n", strings.Join(args, " "))
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = repo
	cmd.Stdout = output
	cmd.Stderr = output
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("affected package tests failed: %w", err)
	}
	return nil
}

func buildReport(vcs, base string, threshold float64, profile *coverageProfile, functions []functionResult) report {
	report := report{VCS: vcs, Base: base, Threshold: threshold, Functions: functions}
	if profile != nil {
		report.CoverageFormat = profile.Format
	} else {
		report.CoverageFormat = "none"
	}
	for _, function := range functions {
		report.AverageCRAP += function.CRAP
		if function.CRAP > report.MaximumCRAP {
			report.MaximumCRAP = function.CRAP
		}
		if function.Crappy {
			report.CrappyFunctions++
		}
	}
	if len(functions) > 0 {
		report.AverageCRAP /= float64(len(functions))
		report.CrappyFunctionRate = float64(report.CrappyFunctions) / float64(len(functions))
	}
	return report
}

func writeTextReport(output io.Writer, report report, allBranches bool) {
	if len(report.Functions) == 0 {
		fmt.Fprintf(output, "No changed Go functions found using %s against %s.\n", report.VCS, report.Base)
		return
	}
	fmt.Fprintf(output, "VCS: %s; base: %s\n\n", report.VCS, report.Base)
	writer := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	fmt.Fprintln(writer, "CRAP\tCOVERAGE\tCC\tBRANCH C/U/?\tFUNCTION\tLOCATION")
	for _, function := range report.Functions {
		coverage := fmt.Sprintf("%.1f%%", function.Coverage*100)
		if !function.CoverageAvailable {
			coverage = "n/a (0%*)"
		}
		fmt.Fprintf(writer, "%.2f\t%s\t%d\t%d/%d/%d\t%s\t%s:%d\n",
			function.CRAP, coverage, function.Complexity, function.CoveredBranches, function.UncoveredBranches,
			function.UnknownBranches, function.Name, function.Path, function.Line)
		for _, branch := range function.Branches {
			if !allBranches && branch.Status != branchUncovered {
				continue
			}
			detail := string(branch.Status)
			if branch.Reason != "" {
				detail += ": " + branch.Reason
			}
			fmt.Fprintf(writer, "\t\t\t\t  %s\tline %d: %s\n", detail, branch.Line, branch.Label)
		}
	}
	_ = writer.Flush()
	fmt.Fprintf(output, "\n%d changed function(s); average CRAP %.2f; maximum %.2f; %d (%.1f%%) at or above %.2f.\n",
		len(report.Functions), report.AverageCRAP, report.MaximumCRAP, report.CrappyFunctions,
		report.CrappyFunctionRate*100, report.Threshold)
	if report.CoverageFormat == "none" {
		fmt.Fprintln(output, "* No coverage profile was supplied; CRAP conservatively used 0% coverage.")
	}
}
