package main

import (
	"strings"
	"testing"
)

func TestParseGoCoverage(t *testing.T) {
	profile, err := parseCoverageProfile(strings.NewReader(`mode: count
example.test/project/internal/a.go:3.2,5.3 2 4
example.test/project/internal/a.go:7.2,7.10 1 0
`))
	if err != nil {
		t.Fatal(err)
	}
	if profile.Format != "go" || len(profile.Files["example.test/project/internal/a.go"]) != 2 {
		t.Fatalf("unexpected profile: %#v", profile)
	}
	if !profile.CanCompareCounts {
		t.Fatal("count-mode profile should permit counter comparison")
	}
	block := profile.Files["example.test/project/internal/a.go"][0]
	if block.Statements != 2 || block.Count != 4 || block.Start.Line != 3 || block.End.Column != 3 {
		t.Fatalf("unexpected block: %#v", block)
	}
}

func TestParseLCOV(t *testing.T) {
	profile, err := parseCoverageProfile(strings.NewReader(`TN:
SF:/repo/a.go
DA:3,2
DA:4,0
BRDA:3,0,0,1
BRDA:3,0,1,-
end_of_record
`))
	if err != nil {
		t.Fatal(err)
	}
	if profile.Format != "lcov" || len(profile.Files["/repo/a.go"]) != 2 || len(profile.Branches["/repo/a.go"]) != 2 {
		t.Fatalf("unexpected profile: %#v", profile)
	}
	if profile.Branches["/repo/a.go"][1].Taken != 0 {
		t.Fatalf("unexecuted branch should have a zero count: %#v", profile.Branches["/repo/a.go"][1])
	}
}

func TestSetCoverageCannotCompareCounts(t *testing.T) {
	profile, err := parseCoverageProfile(strings.NewReader("mode: set\na.go:1.1,1.2 1 1\n"))
	if err != nil {
		t.Fatal(err)
	}
	if profile.CanCompareCounts {
		t.Fatal("set-mode profile should not permit counter comparison")
	}
}

func TestNormalizeProfilePath(t *testing.T) {
	known := []string{"internal/a.go", "a.go"}
	if got := normalizeProfilePath("example.test/project/internal/a.go", "/repo", known); got != "internal/a.go" {
		t.Fatalf("normalizeProfilePath() = %q", got)
	}
	if got := normalizeProfilePath("/repo/internal/a.go", "/repo", known); got != "internal/a.go" {
		t.Fatalf("absolute normalizeProfilePath() = %q", got)
	}
}
