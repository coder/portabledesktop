# portabledesktop — Go Binary

A standalone Go binary that embeds the compressed runtime tarball at compile
time, unpacks it to an XDG cache directory on first run, and manages desktop
sessions directly.

## Quick Start

### Development (without embedded runtime)

```bash
# Build without embedded runtime
make build-dev

# Set runtime directory manually
export PORTABLEDESKTOP_RUNTIME_DIR=/path/to/runtime/output
./dist/portabledesktop up --json
```

### Production (with embedded runtime)

```bash
# Build Nix runtime + embed + compile
make build

# The binary is self-contained
./dist/portabledesktop up --json
```

## Build Targets

| Target              | Description                                    |
|---------------------|------------------------------------------------|
| `make runtime`      | Build Nix runtime tarball                      |
| `make viewer`       | Build noVNC viewer JS bundle                   |
| `make embed`        | Copy runtime + viewer into embed paths         |
| `make build`        | Build with embedded runtime                    |
| `make build-dev`    | Build without embedded runtime                 |
| `make build-linux-amd64` | Cross-compile for linux/amd64             |
| `make build-linux-arm64` | Cross-compile for linux/arm64             |
| `make test`         | Run all tests                                  |
| `make vet`          | Run go vet                                     |
| `make clean`        | Clean build artifacts                          |

## Environment Variables

| Variable                        | Description                           |
|---------------------------------|---------------------------------------|
| `PORTABLEDESKTOP_RUNTIME_DIR`   | Skip unpack, use this runtime dir     |
| `PORTABLEDESKTOP_STATE_FILE`    | Override default state file path      |

## Cache Directory

The runtime is unpacked to:
```
$XDG_CACHE_HOME/portabledesktop/runtime-<sha256-prefix>/
```

Falls back to `~/.cache/portabledesktop/` if `XDG_CACHE_HOME` is unset.

Clean stale caches with:
```bash
portabledesktop cache clean
```

Use `--dry-run` to preview what would be removed.

## Subcommands

| Command             | Description                          |
|---------------------|--------------------------------------|
| `up`                | Start desktop session                |
| `down`              | Stop desktop session                 |
| `info`              | Print session info                   |
| `open`              | Launch detached process              |
| `run`               | Run process, capture output          |
| `screenshot`        | Capture PNG                          |
| `record`            | Record MP4 (Ctrl+C to stop)         |
| `viewer`            | HTTP/WS VNC viewer                   |
| `mouse move`        | Move cursor                          |
| `mouse click`       | Click button                         |
| `mouse down/up`     | Press/release button                 |
| `mouse scroll`      | Scroll                               |
| `keyboard type`     | Type text                            |
| `keyboard key`      | Key combo                            |
| `keyboard down/up`  | Press/release key                    |
| `cursor`            | Print cursor position                |
| `background`        | Set solid background color           |
| `background-image`  | Set background image                 |
| `cache clean`       | Remove cached runtime dirs           |
