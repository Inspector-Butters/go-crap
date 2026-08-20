package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var hunkHeader = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@`)

type vcsBackend string

const (
	vcsAuto vcsBackend = "auto"
	vcsGit  vcsBackend = "git"
	vcsJJ   vcsBackend = "jj"
)

type repositoryInfo struct {
	Root    string
	Backend vcsBackend
}

func resolveRepository(ctx context.Context, path string, requested vcsBackend) (repositoryInfo, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return repositoryInfo{}, err
	}
	if requested != vcsAuto && requested != vcsGit && requested != vcsJJ {
		return repositoryInfo{}, fmt.Errorf("unsupported VCS %q (want auto, git, or jj)", requested)
	}

	backends := []vcsBackend{vcsGit, vcsJJ}
	if hasAncestorMarker(abs, ".jj") {
		backends = []vcsBackend{vcsJJ, vcsGit}
	}
	if requested != vcsAuto {
		backends = []vcsBackend{requested}
	}
	var failures []string
	for _, backend := range backends {
		var root string
		var rootErr error
		switch backend {
		case vcsGit:
			root, rootErr = gitRepositoryRoot(ctx, abs)
		case vcsJJ:
			root, rootErr = jjRepositoryRoot(ctx, abs)
		}
		if rootErr == nil {
			return repositoryInfo{Root: root, Backend: backend}, nil
		}
		failures = append(failures, fmt.Sprintf("%s: %v", backend, rootErr))
	}
	return repositoryInfo{}, fmt.Errorf("find repository root: %s", strings.Join(failures, "; "))
}

func hasAncestorMarker(path, marker string) bool {
	for {
		if _, err := os.Stat(filepath.Join(path, marker)); err == nil {
			return true
		}
		parent := filepath.Dir(path)
		if parent == path {
			return false
		}
		path = parent
	}
}

func gitRepositoryRoot(ctx context.Context, path string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", path, "rev-parse", "--show-toplevel")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return filepath.Clean(strings.TrimSpace(string(out))), nil
}

func jjRepositoryRoot(ctx context.Context, path string) (string, error) {
	cmd := exec.CommandContext(ctx, "jj", "--repository", path, "--no-pager", "--color=never", "workspace", "root")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return filepath.Clean(strings.TrimSpace(string(out))), nil
}

func repositoryChanges(ctx context.Context, repository repositoryInfo, base string) (changeSet, error) {
	switch repository.Backend {
	case vcsGit:
		return gitChanges(ctx, repository.Root, base)
	case vcsJJ:
		return jjChanges(ctx, repository.Root, base)
	default:
		return nil, fmt.Errorf("unsupported VCS %q", repository.Backend)
	}
}

func gitChanges(ctx context.Context, repo, base string) (changeSet, error) {
	if base == "" || strings.HasPrefix(base, "-") {
		return nil, fmt.Errorf("invalid base revision %q", base)
	}
	cmd := exec.CommandContext(ctx, "git", "-C", repo, "-c", "core.quotePath=false", "diff", "--no-ext-diff", "--unified=0", "--diff-filter=ACMR", base, "--", "*.go")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git diff against %q: %w: %s", base, err, strings.TrimSpace(string(out)))
	}
	changes, err := parseUnifiedDiff(out)
	if err != nil {
		return nil, err
	}

	untracked := exec.CommandContext(ctx, "git", "-C", repo, "-c", "core.quotePath=false", "ls-files", "--others", "--exclude-standard", "--", "*.go")
	untrackedOut, err := untracked.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("list untracked Go files: %w: %s", err, strings.TrimSpace(string(untrackedOut)))
	}
	for _, path := range strings.Split(strings.TrimSpace(string(untrackedOut)), "\n") {
		if path == "" {
			continue
		}
		data, readErr := os.ReadFile(filepath.Join(repo, filepath.FromSlash(path)))
		if readErr != nil {
			return nil, fmt.Errorf("read untracked file %s: %w", path, readErr)
		}
		lines := bytes.Count(data, []byte{'\n'})
		if len(data) > 0 && data[len(data)-1] != '\n' {
			lines++
		}
		if lines > 0 {
			changes[filepath.ToSlash(path)] = []lineRange{{Start: 1, End: lines}}
		}
	}
	return changes, nil
}

func jjChanges(ctx context.Context, repo, base string) (changeSet, error) {
	if base == "" || strings.HasPrefix(base, "-") {
		return nil, fmt.Errorf("invalid base revision %q", base)
	}
	args := []string{"--repository", repo, "--no-pager", "--color=never", "diff", "--git", "--context", "0"}
	if base == "HEAD" {
		// This is the jj equivalent of Git's default HEAD-to-working-tree
		// comparison and correctly handles a working-copy merge commit.
		args = append(args, "--revisions", "@")
	} else {
		args = append(args, "--from", base, "--to", "@")
	}
	cmd := exec.CommandContext(ctx, "jj", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("jj diff against %q: %w: %s", base, err, strings.TrimSpace(string(out)))
	}
	changes, err := parseUnifiedDiff(out)
	if err != nil {
		return nil, err
	}
	return changes, nil
}

func parseUnifiedDiff(diff []byte) (changeSet, error) {
	changes := make(changeSet)
	var current string
	scanner := bufio.NewScanner(bytes.NewReader(diff))
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "+++ ") {
			current = strings.TrimPrefix(line, "+++ ")
			if tab := strings.IndexByte(current, '\t'); tab >= 0 {
				current = current[:tab]
			}
			if current == "/dev/null" {
				current = ""
			} else {
				current = strings.TrimPrefix(current, "b/")
				current = filepath.ToSlash(current)
			}
			continue
		}
		match := hunkHeader.FindStringSubmatch(line)
		if match == nil || current == "" {
			continue
		}
		start, _ := strconv.Atoi(match[1])
		count := 1
		if match[2] != "" {
			count, _ = strconv.Atoi(match[2])
		}
		if count == 0 {
			// A deletion has no new-side line. Keep both neighboring lines so a
			// deletion at either edge of a function still selects that function.
			if start > 1 {
				changes[current] = append(changes[current], lineRange{Start: start - 1, End: start})
			} else {
				changes[current] = append(changes[current], lineRange{Start: 1, End: 1})
			}
		} else {
			changes[current] = append(changes[current], lineRange{Start: start, End: start + count - 1})
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("parse unified diff: %w", err)
	}
	for path, ranges := range changes {
		changes[path] = mergeLineRanges(ranges)
	}
	return changes, nil
}

func mergeLineRanges(ranges []lineRange) []lineRange {
	if len(ranges) < 2 {
		return ranges
	}
	sort.Slice(ranges, func(i, j int) bool { return ranges[i].Start < ranges[j].Start })
	merged := []lineRange{ranges[0]}
	for _, r := range ranges[1:] {
		last := &merged[len(merged)-1]
		if r.Start <= last.End+1 {
			if r.End > last.End {
				last.End = r.End
			}
			continue
		}
		merged = append(merged, r)
	}
	return merged
}
