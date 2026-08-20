GO ?= go
VERSION ?= 0.3.2
LDFLAGS = -s -w -X main.version=$(VERSION)

.PHONY: build test release clean

build:
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o go-crap .

test:
	$(GO) test -race ./...
	$(GO) vet ./...

release:
	mkdir -p dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o dist/go-crap-linux-amd64 .
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o dist/go-crap-linux-arm64 .
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o dist/go-crap-darwin-amd64 .
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o dist/go-crap-darwin-arm64 .

clean:
	rm -rf dist go-crap
