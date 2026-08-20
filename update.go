package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	latestVersionURL = "https://proxy.golang.org/github.com/%21inspector-%21butters/go-crap/@latest"
	upgradeCommand   = "go install github.com/Inspector-Butters/go-crap@latest"
)

type httpDoer interface {
	Do(*http.Request) (*http.Response, error)
}

var runUpdateCheck = defaultUpdateCheck

func defaultUpdateCheck(ctx context.Context, output io.Writer) {
	if os.Getenv("GO_CRAP_NO_UPDATE_CHECK") != "" {
		return
	}
	client := &http.Client{Timeout: 2 * time.Second}
	checkForUpdate(ctx, output, client, latestVersionURL, version)
}

func checkForUpdate(ctx context.Context, output io.Writer, client httpDoer, endpoint, current string) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return
	}
	request.Header.Set("User-Agent", "go-crap/"+current)
	request.Header.Set("Accept", "application/json")

	response, err := client.Do(request)
	if err != nil {
		return
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return
	}

	var release struct {
		Version string `json:"Version"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&release); err != nil {
		return
	}
	if isNewerVersion(current, release.Version) {
		fmt.Fprintf(output, "go-crap: warning: %s is available (running %s); update with: %s\n",
			displayVersion(release.Version), displayVersion(current), upgradeCommand)
	}
}

func isNewerVersion(current, latest string) bool {
	currentParts, currentPrerelease, currentOK := parseVersion(current)
	latestParts, latestPrerelease, latestOK := parseVersion(latest)
	if !currentOK || !latestOK {
		return false
	}
	for index := range currentParts {
		if latestParts[index] != currentParts[index] {
			return latestParts[index] > currentParts[index]
		}
	}
	return currentPrerelease && !latestPrerelease
}

func parseVersion(value string) ([3]int, bool, bool) {
	var parts [3]int
	value = strings.TrimPrefix(strings.TrimSpace(value), "v")
	value, _, _ = strings.Cut(value, "+")
	numeric, prerelease, hasPrerelease := strings.Cut(value, "-")
	segments := strings.Split(numeric, ".")
	if len(segments) != len(parts) {
		return parts, false, false
	}
	for index, segment := range segments {
		parsed, err := strconv.Atoi(segment)
		if err != nil || parsed < 0 {
			return parts, false, false
		}
		parts[index] = parsed
	}
	return parts, hasPrerelease && prerelease != "", true
}

func displayVersion(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "v") {
		return value
	}
	return "v" + value
}
