package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCheckForUpdateWarnsAboutNewerRelease(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if got := request.Header.Get("User-Agent"); got != "go-crap/0.3.0" {
			t.Errorf("User-Agent = %q", got)
		}
		fmt.Fprintln(response, `{"Version":"v0.4.0"}`)
	}))
	defer server.Close()

	var output bytes.Buffer
	checkForUpdate(context.Background(), &output, server.Client(), server.URL, "0.3.0")
	warning := output.String()
	if !strings.Contains(warning, "v0.4.0 is available (running v0.3.0)") {
		t.Fatalf("unexpected warning: %q", warning)
	}
	if !strings.Contains(warning, upgradeCommand) {
		t.Fatalf("warning does not contain upgrade command: %q", warning)
	}
}

func TestCheckForUpdateStaysSilentWithoutNewerRelease(t *testing.T) {
	for _, latest := range []string{"v0.3.0", "v0.2.9", "not-a-version"} {
		t.Run(latest, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				fmt.Fprintf(response, `{"Version":%q}`, latest)
			}))
			defer server.Close()

			var output bytes.Buffer
			checkForUpdate(context.Background(), &output, server.Client(), server.URL, "0.3.0")
			if output.Len() != 0 {
				t.Fatalf("unexpected warning: %q", output.String())
			}
		})
	}
}

func TestIsNewerVersion(t *testing.T) {
	tests := []struct {
		current string
		latest  string
		want    bool
	}{
		{"0.3.0", "v0.3.1", true},
		{"v0.3.9", "v0.4.0", true},
		{"1.9.9", "v2.0.0", true},
		{"0.3.0-rc.1", "v0.3.0", true},
		{"0.3.0", "v0.3.0", false},
		{"0.4.0", "v0.3.9", false},
		{"development", "v9.0.0", false},
	}
	for _, test := range tests {
		name := test.current + "_to_" + test.latest
		t.Run(name, func(t *testing.T) {
			if got := isNewerVersion(test.current, test.latest); got != test.want {
				t.Fatalf("isNewerVersion(%q, %q) = %v, want %v", test.current, test.latest, got, test.want)
			}
		})
	}
}
