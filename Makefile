.PHONY: runtime viewer embed build build-dev build-linux-amd64 build-linux-arm64 test vet clean

# Build the Nix runtime tarball (already at repo root).
runtime:
	./scripts/build.sh

# Build the noVNC viewer JS bundle and copy it for embedding.
viewer:
	cd viewer && bun install --frozen-lockfile && bun run build
	cp viewer/dist/viewer-client.js pd/internal/viewer/viewer-client.js

# Compress the runtime tarball and place it for embedding.
# The Nix build produces an uncompressed output.tar; we compress
# it with zstd so the Go binary can embed a smaller archive.
embed: runtime viewer
	zstd -T0 -3 --force result/output.tar -o pd/cmd/portabledesktop/runtime.tar.zst

# Build the Go binary (with embedded runtime).
build: embed
	cd pd && CGO_ENABLED=0 go build -tags embed_runtime -o dist/portabledesktop ./cmd/portabledesktop

# Build without embedded runtime (for development).
build-dev:
	cd pd && CGO_ENABLED=0 go build -o dist/portabledesktop ./cmd/portabledesktop

# Cross-compile targets.
build-linux-amd64: embed
	cd pd && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -tags embed_runtime -o dist/portabledesktop-linux-amd64 ./cmd/portabledesktop

build-linux-arm64: embed
	cd pd && GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -tags embed_runtime -o dist/portabledesktop-linux-arm64 ./cmd/portabledesktop

# All tests (unit + integration + e2e). Requires runtime available.
test:
	cd pd && go test -race -count=1 -timeout=180s ./...

# Lint.
vet:
	cd pd && go vet ./...

# Clean build artifacts.
clean:
	rm -rf pd/dist/
	rm -f pd/cmd/portabledesktop/runtime.tar.zst
	rm -f pd/internal/viewer/viewer-client.js
