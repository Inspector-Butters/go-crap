package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var goCoverageLine = regexp.MustCompile(`^(.+):(\d+)\.(\d+),(\d+)\.(\d+)\s+(\d+)\s+(\d+)$`)

func loadCoverageProfile(path, repo string, knownFiles []string) (*coverageProfile, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open coverage profile: %w", err)
	}
	defer file.Close()
	profile, err := parseCoverageProfile(file)
	if err != nil {
		return nil, fmt.Errorf("parse coverage profile: %w", err)
	}
	profile.normalizePaths(repo, knownFiles)
	return profile, nil
}

func parseCoverageProfile(reader io.Reader) (*coverageProfile, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)
	var lines []string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			lines = append(lines, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(lines) == 0 {
		return nil, fmt.Errorf("profile is empty")
	}
	if strings.HasPrefix(lines[0], "mode:") {
		return parseGoCoverage(lines)
	}
	for _, line := range lines {
		if strings.HasPrefix(line, "SF:") || strings.HasPrefix(line, "TN:") {
			return parseLCOV(lines)
		}
	}
	return nil, fmt.Errorf("unrecognized profile format")
}

func parseGoCoverage(lines []string) (*coverageProfile, error) {
	mode := strings.TrimSpace(strings.TrimPrefix(lines[0], "mode:"))
	if mode != "set" && mode != "count" && mode != "atomic" {
		return nil, fmt.Errorf("unsupported Go coverage mode %q", mode)
	}
	profile := &coverageProfile{
		Format:           "go",
		CanCompareCounts: mode == "count" || mode == "atomic",
		Files:            make(map[string][]coverageBlock),
		Branches:         make(map[string][]explicitBranch),
	}
	for lineNumber, line := range lines[1:] {
		match := goCoverageLine.FindStringSubmatch(line)
		if match == nil {
			return nil, fmt.Errorf("line %d: malformed Go coverage record", lineNumber+2)
		}
		values := make([]int64, 6)
		for i := range values {
			value, err := strconv.ParseInt(match[i+2], 10, 64)
			if err != nil {
				return nil, fmt.Errorf("line %d: %w", lineNumber+2, err)
			}
			values[i] = value
		}
		profile.Files[filepath.ToSlash(match[1])] = append(profile.Files[filepath.ToSlash(match[1])], coverageBlock{
			Start:      sourcePosition{Line: int(values[0]), Column: int(values[1])},
			End:        sourcePosition{Line: int(values[2]), Column: int(values[3])},
			Statements: int(values[4]),
			Count:      values[5],
		})
	}
	return profile, nil
}

func parseLCOV(lines []string) (*coverageProfile, error) {
	profile := &coverageProfile{Format: "lcov", CanCompareCounts: true, Files: make(map[string][]coverageBlock), Branches: make(map[string][]explicitBranch)}
	var current string
	for lineNumber, line := range lines {
		switch {
		case strings.HasPrefix(line, "SF:"):
			current = filepath.ToSlash(strings.TrimPrefix(line, "SF:"))
		case strings.HasPrefix(line, "DA:"):
			if current == "" {
				return nil, fmt.Errorf("line %d: DA record before SF", lineNumber+1)
			}
			parts := strings.Split(strings.TrimPrefix(line, "DA:"), ",")
			if len(parts) < 2 {
				return nil, fmt.Errorf("line %d: malformed DA record", lineNumber+1)
			}
			lineValue, err := strconv.Atoi(parts[0])
			if err != nil {
				return nil, fmt.Errorf("line %d: %w", lineNumber+1, err)
			}
			count, err := strconv.ParseInt(parts[1], 10, 64)
			if err != nil {
				return nil, fmt.Errorf("line %d: %w", lineNumber+1, err)
			}
			profile.Files[current] = append(profile.Files[current], coverageBlock{
				Start:      sourcePosition{Line: lineValue, Column: 1},
				End:        sourcePosition{Line: lineValue, Column: int(^uint(0) >> 1)},
				Statements: 1,
				Count:      count,
			})
		case strings.HasPrefix(line, "BRDA:"):
			if current == "" {
				return nil, fmt.Errorf("line %d: BRDA record before SF", lineNumber+1)
			}
			parts := strings.Split(strings.TrimPrefix(line, "BRDA:"), ",")
			if len(parts) != 4 {
				return nil, fmt.Errorf("line %d: malformed BRDA record", lineNumber+1)
			}
			branchLine, err := strconv.Atoi(parts[0])
			if err != nil {
				return nil, fmt.Errorf("line %d: %w", lineNumber+1, err)
			}
			var taken int64
			if parts[3] != "-" {
				taken, err = strconv.ParseInt(parts[3], 10, 64)
				if err != nil {
					return nil, fmt.Errorf("line %d: %w", lineNumber+1, err)
				}
			}
			profile.Branches[current] = append(profile.Branches[current], explicitBranch{Line: branchLine, Block: parts[1], Branch: parts[2], Taken: taken})
		}
	}
	return profile, nil
}

func (profile *coverageProfile) normalizePaths(repo string, knownFiles []string) {
	files := make(map[string][]coverageBlock)
	for raw, blocks := range profile.Files {
		path := normalizeProfilePath(raw, repo, knownFiles)
		files[path] = append(files[path], blocks...)
	}
	branches := make(map[string][]explicitBranch)
	for raw, entries := range profile.Branches {
		path := normalizeProfilePath(raw, repo, knownFiles)
		branches[path] = append(branches[path], entries...)
	}
	profile.Files = files
	profile.Branches = branches
}

func normalizeProfilePath(raw, repo string, knownFiles []string) string {
	clean := filepath.ToSlash(filepath.Clean(raw))
	if filepath.IsAbs(raw) {
		if relative, err := filepath.Rel(repo, raw); err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return filepath.ToSlash(relative)
		}
	}
	clean = strings.TrimPrefix(clean, "./")
	// Go profiles normally use module import paths. Match the longest known
	// repository-relative suffix without needing to understand nested modules.
	sort.SliceStable(knownFiles, func(i, j int) bool { return len(knownFiles[i]) > len(knownFiles[j]) })
	for _, known := range knownFiles {
		known = filepath.ToSlash(known)
		if clean == known || strings.HasSuffix(clean, "/"+known) {
			return known
		}
	}
	return clean
}

func (profile *coverageProfile) hasFile(path string) bool {
	_, ok := profile.Files[path]
	return ok
}
