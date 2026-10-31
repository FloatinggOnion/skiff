.PHONY: test e2e vet lint fmt fmt-check build dash install

# Run the Go test suite.
test:
	go test ./...

# Deploy a git repo end to end through the real binary (needs Docker).
e2e:
	go test -tags e2e -count=1 -v ./e2e/...

vet:
	go vet ./...

# Lint with golangci-lint (config in .golangci.yml). Install:
#   go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
lint:
	golangci-lint run

# Format all Go sources in place.
fmt:
	gofmt -w .

# Fail if anything isn't gofmt-clean (used in CI).
fmt-check:
	@out=$$(gofmt -l . | grep -v '^\.agents/' || true); \
	if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

# Build the CLI (uses the already-embedded dashboard).
build:
	go build -o skiff .

install:
	go install .

# Rebuild the dashboard and sync it into the Go binary's embed dir.
dash:
	cd web/dash && npm run build
	rm -rf internal/panel/dist && cp -r web/dash/dist internal/panel/dist
