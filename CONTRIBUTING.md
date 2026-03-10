## Contributing to `portabledesktop`

### Scope

This repository has two primary deliverables:

1. The Go binary (`portabledesktop`) — standalone CLI.
2. The Nix runtime artifact (`result/output.tar`, `result/output/`, `result/manifest.json`).

### Local Setup

```bash
# Build viewer JS (requires Bun)
make viewer

# Build without embedded runtime (for development)
make build-dev

# Set runtime directory manually
export PORTABLEDESKTOP_RUNTIME_DIR=/path/to/runtime/output
./pd/dist/portabledesktop up --json
```

For full builds with embedded runtime:

```bash
./scripts/build.sh    # Build Nix runtime
make build            # Builds runtime + viewer + Go binary
```

For broader compatibility checks:

```bash
./scripts/matrix.sh --skip-build
```

### Testing

```bash
make vet
make test
```

### Pull Requests

Before opening a PR:

1. Run `make vet`.
2. Run `make test`.
3. Run `make build-dev` (or `make build` for full build).
4. If runtime-related, run `./scripts/smoke.sh --skip-build`.
5. If runtime-related, run `./scripts/matrix.sh --skip-build`.

Include in your PR description:

1. Behavior change summary.
2. Compatibility impact (x64/arm64, distro notes).
3. Exact commands used for verification.

### Release Model

Releases are tag-driven:

1. Create and push tag `vX.Y.Z`.
2. GitHub Actions builds Go binaries + GitHub release artifacts.
