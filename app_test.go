package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestMain(m *testing.M) {
	runUpdateCheck = func(context.Context, io.Writer) {}
	os.Exit(m.Run())
}

func TestRunChecksForUpdates(t *testing.T) {
	original := runUpdateCheck
	defer func() { runUpdateCheck = original }()
	called := false
	runUpdateCheck = func(context.Context, io.Writer) { called = true }

	var stdout, stderr bytes.Buffer
	if exitCode := run(context.Background(), []string{"-version"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("run(-version) exit = %d, stderr: %s", exitCode, stderr.String())
	}
	if !called {
		t.Fatal("run did not check for updates")
	}
}

func TestHelpExitsSuccessfully(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if exitCode := run(context.Background(), []string{"-h"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("run(-h) exit = %d, stderr: %s", exitCode, stderr.String())
	}
	if !bytes.Contains(stderr.Bytes(), []byte("usage: go-crap")) {
		t.Fatalf("help output missing usage: %s", stderr.String())
	}
}

func TestRunWithGitDiffAndExistingProfile(t *testing.T) {
	repo := t.TempDir()
	writeTestFile(t, filepath.Join(repo, "go.mod"), "module fixture.test/project\n\ngo 1.22\n")
	writeTestFile(t, filepath.Join(repo, "calc.go"), "package fixture\n\nfunc Calc(x int) int { return x }\n")
	initializeTestRepository(t, repo)

	writeTestFile(t, filepath.Join(repo, "calc.go"), `package fixture

func Calc(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
`)
	profile := filepath.Join(repo, "coverage.out")
	writeTestFile(t, profile, `mode: count
fixture.test/project/calc.go:3.22,4.10 1 1
fixture.test/project/calc.go:5.3,5.12 1 1
fixture.test/project/calc.go:7.2,7.10 1 0
`)
	var stdout, stderr bytes.Buffer
	exitCode := run(context.Background(), []string{
		"-repo", repo,
		"-base", "HEAD",
		"-coverprofile", profile,
		"-json",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("run() exit = %d, stderr: %s", exitCode, stderr.String())
	}
	var got report
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("decode report: %v\n%s", err, stdout.String())
	}
	if len(got.Functions) != 1 || got.Functions[0].Name != "Calc" || got.Functions[0].Complexity != 2 {
		t.Fatalf("unexpected report: %#v", got)
	}
	if got.CoverageFormat != "go" || !got.Functions[0].CoverageAvailable {
		t.Fatalf("unexpected coverage result: %#v", got.Functions[0])
	}
}

func TestRunCollectsGoCoverageForAffectedPackage(t *testing.T) {
	repo := t.TempDir()
	writeTestFile(t, filepath.Join(repo, "go.mod"), "module fixture.test/project\n\ngo 1.22\n")
	writeTestFile(t, filepath.Join(repo, "calc.go"), "package fixture\n\nfunc Calc(x int) int { return x }\n")
	initializeTestRepository(t, repo)

	writeTestFile(t, filepath.Join(repo, "calc.go"), `package fixture

func Calc(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
`)
	writeTestFile(t, filepath.Join(repo, "calc_test.go"), `package fixture

import "testing"

func TestCalc(t *testing.T) {
	if Calc(-2) != 2 || Calc(2) != 2 {
		t.Fatal("unexpected result")
	}
}
`)
	t.Setenv("PATH", filepath.Join(runtime.GOROOT(), "bin")+string(os.PathListSeparator)+os.Getenv("PATH"))
	var stdout, stderr bytes.Buffer
	exitCode := run(context.Background(), []string{"-repo", repo, "-base", "HEAD", "-json"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("run() exit = %d, stderr: %s", exitCode, stderr.String())
	}
	var got report
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("decode report: %v\n%s", err, stdout.String())
	}
	if len(got.Functions) != 1 {
		t.Fatalf("unexpected report: %#v", got)
	}
	function := got.Functions[0]
	if function.Coverage != 1 || function.CRAP != 2 || function.CoveredBranches != 2 {
		t.Fatalf("unexpected function result: %#v\nstderr: %s", function, stderr.String())
	}
}

func TestRunWithNativeJJWorkspace(t *testing.T) {
	if _, err := exec.LookPath("jj"); err != nil {
		t.Skip("jj is not installed")
	}
	parent := t.TempDir()
	repo := filepath.Join(parent, "repo")
	runJJ(t, parent, "git", "init", "--no-colocate", repo)
	writeTestFile(t, filepath.Join(repo, "go.mod"), "module fixture.test/project\n\ngo 1.22\n")
	writeTestFile(t, filepath.Join(repo, "calc.go"), "package fixture\n\nfunc Calc(x int) int { return x }\n")
	runJJ(t, repo, "describe", "-m", "base")
	runJJ(t, repo, "new")

	writeTestFile(t, filepath.Join(repo, "calc.go"), `package fixture

func Calc(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
`)
	profile := filepath.Join(parent, "coverage.out")
	writeTestFile(t, profile, `mode: count
fixture.test/project/calc.go:3.22,4.10 1 1
fixture.test/project/calc.go:5.3,5.12 1 1
fixture.test/project/calc.go:7.2,7.10 1 0
`)
	var stdout, stderr bytes.Buffer
	exitCode := run(context.Background(), []string{
		"-repo", repo,
		"-base", "HEAD",
		"-coverprofile", profile,
		"-json",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("run() exit = %d, stderr: %s", exitCode, stderr.String())
	}
	var got report
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("decode report: %v\n%s", err, stdout.String())
	}
	if got.VCS != "jj" || len(got.Functions) != 1 || got.Functions[0].Name != "Calc" {
		t.Fatalf("unexpected jj report: %#v", got)
	}
	if _, err := os.Stat(filepath.Join(repo, ".git")); !os.IsNotExist(err) {
		t.Fatalf("test workspace unexpectedly has a colocated .git directory: %v", err)
	}
}

func initializeTestRepository(t *testing.T, repo string) {
	t.Helper()
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.email", "test@example.com")
	runGit(t, repo, "config", "user.name", "Test")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "base")
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}

func runJJ(t *testing.T, dir string, args ...string) {
	t.Helper()
	baseArgs := []string{
		"--no-pager",
		"--color=never",
		"--config", "user.name=Test",
		"--config", "user.email=test@example.com",
	}
	cmd := exec.Command("jj", append(baseArgs, args...)...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("jj %v: %v: %s", args, err, output)
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
