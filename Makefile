# Mostlydone build. `make build` needs Node (npm) and Go.

GO_LDFLAGS := -s -w

.PHONY: ui build test clean

# Install exact UI dependencies, build into internal/webui/dist, write .gz files.
ui:
	cd web && npm ci && npm run build

build: ui
	CGO_ENABLED=0 go build -trimpath -ldflags "$(GO_LDFLAGS)" -o mostlydone ./cmd/mostlydone

test:
	go test ./...
	cd web && npm run typecheck && npm test

# Remove the built UI (keeps the placeholder) and the binary.
clean:
	find internal/webui/dist -mindepth 1 ! -name .keep -exec rm -rf {} +
	rm -f mostlydone
