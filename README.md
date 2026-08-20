# go-crap

[![CI](https://github.com/Inspector-Butters/go-crap/actions/workflows/ci.yml/badge.svg)](https://github.com/Inspector-Butters/go-crap/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/Inspector-Butters/go-crap.svg)](https://pkg.go.dev/github.com/Inspector-Butters/go-crap)

`go-crap` reports the CRAP (Change Risk Analysis and Predictions) score of each
named Go function or method touched by a Git or Jujutsu diff. It combines
cyclomatic complexity with automated test coverage and audits the branch
outcomes inside each changed function.

```text
CRAP(m) = complexity(m)^2 * (1 - coverage(m))^3 + complexity(m)
```

The formula is Alberto Savoia's original
[CRAP 0.1 definition](https://www.artima.com/weblogs/viewpost.jsp?thread=210575).
The default threshold is 30, matching the initial
[crap4j interpretation](https://www.artima.com/weblogs/viewpost.jsp?thread=215899).

## Install

Go 1.22 or newer is required.

```sh
go install github.com/Inspector-Butters/go-crap@latest
```

Ensure the Go binary directory (usually `~/go/bin`) is on `PATH`, then run:

```sh
go-crap -h
```

Upgrade to the newest release with the same installation command:

```sh
go install github.com/Inspector-Butters/go-crap@latest
```

Every invocation checks GitHub's latest-release redirect and prints a warning
to standard error when a newer release is available. The warning includes an
installation command pinned to that release. Network errors never prevent
analysis. Set `GO_CRAP_NO_UPDATE_CHECK=1` to disable the check in offline or
hermetic environments.

Alternatively, download a prebuilt Linux or macOS archive from the
[latest release](https://github.com/Inspector-Butters/go-crap/releases/latest),
or clone and build from source:

```sh
git clone https://github.com/Inspector-Butters/go-crap.git
cd go-crap
make build
./go-crap -h
```

## Use

Analyze uncommitted work against `HEAD`; tests for packages containing changed
functions run automatically:

```sh
go-crap
```

The VCS is detected automatically. In a jj workspace, `HEAD` means the current
working-copy change relative to its parent(s), matching `jj diff -r @`. Both
native jj workspaces (without `.git`) and colocated jj/Git workspaces work:

```sh
go-crap -vcs jj
go-crap -vcs jj -base 'trunk()'
```

When both `.jj` and `.git` are present, automatic detection prefers jj. Use
`-vcs git` to force Git semantics. Like other jj commands, analysis snapshots
the working copy before calculating its diff.

With Git, analyze all commits and working-tree changes since the target branch:

```sh
go-crap -base origin/main
```

Use an existing Go coverage profile instead of running tests:

```sh
go test -covermode=count -coverprofile=coverage.out ./...
go-crap -base origin/main -coverprofile coverage.out
```

LCOV is accepted too, which is useful for Bazel repositories:

```sh
bazel coverage --combined_report=lcov //path/to/package:go_default_test
go-crap -base HEAD~1 -coverprofile bazel-out/_coverage/_coverage_report.dat
```

For CI, `-fail` returns status 1 when any changed function has a score at or
above `-threshold`. The text report expands uncovered branches by default;
`-all-branches` expands covered and indeterminate outcomes too. `-json` always
emits every branch. Test and generated files are skipped unless
`-include-tests` or `-include-generated` is set. Repeated `-test-arg` values are
passed to `go test`.

## Linux and macOS builds

The tool is pure Go and has no C dependencies. It builds natively on Linux and
macOS, including Linux ARM64 and Apple silicon. To create the complete release
matrix from either development platform:

```sh
make release
```

Locally, this produces:

- `dist/go-crap-linux-amd64`
- `dist/go-crap-linux-arm64`
- `dist/go-crap-darwin-amd64`
- `dist/go-crap-darwin-arm64`

Version tags publish compressed versions of these binaries and a SHA-256
checksum file on the GitHub Releases page automatically.

## Go coverage semantics

CRAP coverage is the statement-weighted basic-block coverage for the function.
For LCOV input it is executable-line coverage. A missing profile entry is
scored conservatively as 0%, and is visibly marked in the report.

Go's native coverage instrumentation approximates basic blocks around control
structures. It does not record exact control-flow edges and deliberately does
not instrument the operands of `&&` and `||` separately. `go-crap` therefore
reports what can be proven for each `if`, loop, switch, select, and short-circuit
outcome:

- `covered`: the corresponding instrumented block ran;
- `uncovered`: the corresponding block was instrumented but did not run;
- `unknown`: Go's profile has no distinct counter for the edge.

An LCOV producer that supplies `BRDA` records gives exact covered/uncovered
branch entries. Without those records, `unknown` is intentional rather than a
false claim of branch coverage. This limitation follows the Go tool's documented
[basic-block coverage model](https://go.dev/blog/cover#TOC_8.).

Cyclomatic complexity starts at one and adds one for every `if`, `for`,
`range`, non-default `case`, non-default `select` clause, `&&`, and `||`.
Nested function literals are excluded from their enclosing function.
