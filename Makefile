.PHONY: runtime viewer embed build build-dev build-linux-amd64 build-linux-arm64 test vet clean runtime-module runtime-module-check

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

# Minimal runtime Go module (runtime/): a static Xvnc built with Docker and
# committed into the module so Go programs can embed it via go.mod. The build
# is byte reproducible, so runtime-module-check can verify the committed
# archives match the Dockerfile.
RUNTIME_MODULE_ARCHES := amd64 arm64

runtime-module:
	for arch in $(RUNTIME_MODULE_ARCHES); do \
		./runtime/build/build.sh --arch "$$arch" --output "runtime/desktop-runtime-linux-$$arch.tar.zst"; \
		./runtime/build/check_size.sh "runtime/desktop-runtime-linux-$$arch.tar.zst"; \
	done

# Rebuild the archive for the current architecture and fail if it differs
# from the committed one. Used by CI.
runtime-module-check:
	arch="$$(uname -m | sed -e 's/x86_64/amd64/' -e 's/aarch64/arm64/')"; \
	tmp="$$(mktemp --suffix=.tar.zst)"; \
	./runtime/build/build.sh --arch "$$arch" --output "$$tmp"; \
	./runtime/build/check_size.sh "$$tmp"; \
	if ! cmp -s "$$tmp" "runtime/desktop-runtime-linux-$$arch.tar.zst"; then \
		echo "error: runtime/desktop-runtime-linux-$$arch.tar.zst does not match the build output; run 'make runtime-module' and commit" >&2; \
		rm -f "$$tmp"; exit 1; \
	fi; \
	rm -f "$$tmp"

