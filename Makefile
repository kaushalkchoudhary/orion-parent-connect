.PHONY: build test vet browser-test

build:
	mkdir -p bin
	CGO_ENABLED=1 go build -trimpath -o bin/parent-portal ./cmd/parent-portal

test:
	go test -race ./...

vet:
	go vet ./...

# Requires Node, Playwright, Chrome and FFmpeg. Never sends mail or contacts cameras.
browser-test:
	PARENT_BROWSER_CHECK=1 go test -count=1 -run TestBrowserFlow -v ./internal/parentportal
