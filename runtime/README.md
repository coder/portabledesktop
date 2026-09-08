# Minimal runtime module

`github.com/coder/portabledesktop/runtime` is a Go module that embeds a minimal,
self-contained Linux desktop runtime: a **fully static** TigerVNC `Xvnc`,
`xkbcomp`, and a trimmed `xkeyboard-config` data set, packed as a zstd
compressed tar archive per architecture. It exists so that Go programs (the
Coder workspace agent is the first consumer) can ship a working X server without
downloading anything at run time.

It is deliberately smaller than the full portabledesktop runtime: no window
manager, dock, wallpaper tools, `xdotool` or `ffmpeg`. Consumers drive the X
server themselves (for example over XTEST and `GetImage`, or through the VNC
protocol) and connect a VNC client such as noVNC to it.

| Target        | Compressed | Unpacked |
|---------------|------------|----------|
| `linux/amd64` | 1.71 MiB   | 4.5 MiB  |
| `linux/arm64` | 1.66 MiB   | 4.5 MiB  |

## Using the module

```go
import pdruntime "github.com/coder/portabledesktop/runtime"

if pdruntime.Available() {
    archive := pdruntime.Archive()     // zstd compressed tar
    digest := pdruntime.SHA256()       // key your unpack cache on this
    // unpack, then exec <dir>/bin/Xvnc with -xkbdir <dir>/share/xkb and
    // <dir>/bin on PATH; see "Relocatability and how to launch it".
}
```

Archives are embedded for `linux/amd64` and `linux/arm64` only. Elsewhere
`Available()` is false and `Archive()` is nil, so the package compiles on every
platform and callers fall back gracefully.

The archives are **committed to this directory**. A Go module is served from git
at a tag, so the bytes have to live in the repository; the build is byte
reproducible, which lets CI prove the committed archives match the `Dockerfile`
(`make runtime-module-check`). Consumers pin the module through `go.mod` and
verify it through `go.sum` like any other dependency, and the consumer's build
is plain `go build` with no container or download step.

## Where Docker fits (and where it does not)

Docker is used **only to build the archives** in `build/`, as a hermetic musl
cross-compilation environment. It is not needed to consume the module and it is
never used on the machine where `Xvnc` runs: the consuming program unpacks the
embedded archive to a directory it owns and execs `bin/Xvnc` directly. No
Docker, no network access, no package manager, and no dependency on host
libraries; the host can be any Linux distribution with any libc, and the user
needs no privileges beyond writing to the unpack directory.

## Rebuilding the archives

```console
$ make runtime-module          # both architectures, into runtime/
$ make runtime-module-check    # rebuild for this host's arch and diff against the committed file
```

or for a single architecture:

```console
$ ./runtime/build/build.sh --arch amd64 --output runtime/desktop-runtime-linux-amd64.tar.zst
$ ./runtime/build/check_size.sh runtime/desktop-runtime-linux-amd64.tar.zst
```

`build.sh` runs the build command (`docker buildx build` by default) with
`--platform linux/<arch>` and exports the scratch stage directly to a directory,
then packs it with a fixed sort order, timestamp and ownership before
compressing with `zstd -19 -T0`. The build image also sets `SOURCE_DATE_EPOCH`,
so two independent `--no-cache` builds of the same sources produce byte
identical archives, including across different builders. `check_size.sh` fails
when the archive exceeds its budget (6 MiB by default).

### Choosing a builder

The build needs a builder that can export a filesystem (`--output type=local`).
Nothing in the `Dockerfile` touches the local Docker daemon, so a remote builder
works as well as a local one; the builder does need outbound network access to
fetch the pinned sources.

| Selection | Behavior |
|---|---|
| `--builder <name>` | Passed straight through to the build command |
| `BUILDX_BUILDER` in the environment | Left alone for buildx to pick up |
| Neither | Uses the current builder, and only falls back to creating a local `portabledesktop-runtime` docker-container builder when that builder cannot export `type=local` |

The fallback is decided by a real capability probe (exporting an empty `FROM
scratch` image, which needs no image pull and takes under 100 ms) rather than by
matching on the driver name. Current Docker daemons export `type=local` fine, so
the fallback normally does not trigger.

### QEMU (local and fork builds only)

On an x86 host, building `linux/arm64` needs QEMU (CI uses native arm64 runners instead):

```shell
docker run --rm --privileged tonistiigi/binfmt --install arm64
```

This is the fallback path for local development and fork builds. It works, but
the whole Alpine toolchain runs emulated, which costs roughly 11x.

### Measured cold build times

No cache, on a 128 core host:

| Target | Buildkit step time | Wall clock | Notes |
|---|---|---|---|
| `linux/amd64` | 57 s | 63 to 74 s | native |
| `linux/arm64` | 665 s (11.1 min) | 685 s (11.4 min) | QEMU, 11.6x slower |

The emulation cost sits in the compile steps: the xorg-server configure, build
and relink goes from 22 s to 295 s, and the static helper libraries go from 13 s
to 252 s. Downloads and `apk add` are barely affected.

Smaller CI runners scale both numbers up, which is why the archives are built
once and committed to the module rather than rebuilt by every consumer. A native
arm64 builder removes the emulation penalty entirely.

## Output layout

```text
bin/Xvnc            statically linked TigerVNC X server
bin/xkbcomp         statically linked keymap compiler
share/xkb/...       trimmed xkeyboard-config data (rules, keycodes, types,
                    compat, symbols, geometry)
manifest.json       schema version, target architecture, component versions
                    and whether the binaries are static
LICENSES/           license text of every bundled component
```

## Relocatability and how to launch it

The tree is unpacked to an arbitrary directory, so `Xvnc` must not need any
build-time absolute path at run time. Three configure options guarantee that:

| Path | Configure option | Runtime behavior |
|---|---|---|
| xkbcomp | `--with-xkb-bin-directory=` (empty) | The server runs plain `xkbcomp`, resolved through `PATH` |
| XKB data | `--with-xkb-path=/usr/share/X11/xkb` | Compiled in, but always overridden with `-xkbdir` |
| Fonts | `--with-default-font-path=built-ins` | The libXfont2 built-in fonts live inside the binary, so no font directory has to exist |

The compiled keymap is written to `/tmp` (`--with-xkb-output=/tmp`), so the
unpacked runtime directory itself never has to be writable.

The launcher therefore has exactly two obligations: prepend the runtime's `bin`
directory to `PATH`, and pass `-xkbdir`. This is the invocation that was
verified on a glibc host:

```console
$ PATH="$runtime/bin:$PATH" "$runtime/bin/Xvnc" :77 \
    -rfbport 5977 \
    -localhost \
    -SecurityTypes None \
    -AlwaysShared \
    -AcceptSetDesktopSize \
    -geometry 1280x800 \
    -depth 24 \
    -xkbdir "$runtime/share/xkb" \
    -desktop portabledesktop
```

No `-fp` flag is needed; passing `-fp built-ins` explicitly is equivalent. Both
variants expose the same six built-in core fonts (`fixed`, `cursor`, `6x13` and
three aliases), which is enough for legacy core-font clients to start.
Applications that render text client side through fontconfig and Xft use the
fonts present on the host and are unaffected.

Both obligations are hard requirements, not best effort:

- Without the runtime's `bin` on `PATH` the server logs `xkbcomp: not found`,
  then `XKB: Failed to compile keymap`, and exits with
  `Fatal server error: Failed to activate virtual core keyboard`.
- Without `-xkbdir` the server falls back to the compiled-in
  `/usr/share/X11/xkb`. On a host that has no xkeyboard-config installed this is
  the same fatal failure; on a host that does have it, the server silently
  compiles the keymap from the host's data instead of ours, which is exactly the
  behavior this runtime exists to avoid.

## What is excluded and why

The runtime is embedded in Go binaries, so every megabyte is paid for on
every download of those binaries. The exclusions below are what keep it under 2 MiB
compressed.

- **GLX, Mesa, DRI, glamor, libdrm.** By far the largest lever. Enabling GLX
  pulls in Mesa, which pulls in LLVM for its software rasterizer, adding
  roughly 180 MB of installed size on Alpine. A VNC session composites in
  software on the client side, so the server needs no GL at all.
- **GnuTLS and nettle.** `Xvnc` listens on localhost only with
  `-SecurityTypes None`. The embedding application authenticates the
  connection before any traffic reaches the X server, so in-server TLS and
  RSA-AES authentication would only duplicate work already done.
- **PAM.** For the same reason there is no in-server password check. PAM also
  makes a static binary impossible because it `dlopen`s its modules at runtime.
  TigerVNC's CMake build requires PAM development files unconditionally, so
  `pam_stub.c` supplies a small archive whose entry points all fail closed, and
  the real shared library is removed from the build image.
- **ffmpeg and H.264.** noVNC decodes Tight, ZRLE and JPEG in the browser, so
  the H.264 encoder and its libav dependencies are dead weight.
- **systemd, SELinux, wayland, pwquality, NLS.** Session management, labelling
  and translations are not used by an application-managed server.
- **The TigerVNC viewer, x0vncserver and vncsession.** Only the `Xvnc` binary
  and the libraries it links are built.
- **FreeType in libXfont2.** The runtime ships no fonts, so the scalable font
  backend (and with it libpng, brotli and bzip2) buys nothing. The built-in and
  PCF bitmap backends remain, which is what the X server needs to start.
  Applications render text client side through fontconfig and Xft using the
  fonts present on the host.
- **Most of xkeyboard-config.** Only the `evdev` rules file, the `evdev` and
  `aliases` keycodes, all types and compat files, the symbol files reachable
  from `pc+us+inet(evdev)`, and the `pc` geometry are kept. The `*.xml` and
  `*.lst` catalogues only feed configuration GUIs. This reduces about 10 MB of
  data to 580 KB. The build compiles the default keymap with the trimmed tree
  and fails if it does not resolve.

Everything is compiled with `-Os -ffunction-sections -fdata-sections`, linked
with `-Wl,--gc-sections`, and stripped.

## Static linking

Both binaries are fully static; `manifest.json` reports `"static": true`. Alpine
ships static archives for zlib, libjpeg-turbo, pixman, libbsd, libX11 and
libxcb, and the Dockerfile builds the remaining ones (libXau, libXdmcp,
libfontenc, libXfont2, libxkbfile, libxcvt and libmd) from source because Alpine
has no `-static` subpackage for them.

Two details make the static link work:

- libtool treats `-static` as "prefer static libtool libraries", so `Xvnc` is
  relinked after the normal build with `-all-static`, which cannot be used
  during `configure` because it is not a compiler flag.
- libbsd's static archive expects the SHA1 symbols that live in libmd, so `-lmd`
  is added to the final link.

Because the binaries are static there is no loader, no `ld-musl-*.so.1` and no
`.so` closure to bundle, and no wrapper script is needed. Static linking was
verified end to end by unpacking the amd64 archive on an Ubuntu (glibc) host and
starting `bin/Xvnc` there: it served the `RFB 003.008` banner on a localhost
port and compiled the `pc+us+inet(evdev)` keymap from the bundled `share/xkb`
data without touching a single library from that host.

Keep it that way when bumping versions. If a future version cannot be linked
statically, the fallback is a dynamic musl binary shipped with
`lib/ld-musl-<arch>.so.1`, the exact `ldd` closure under `lib/`, and a
`bin/Xvnc` wrapper that execs the loader with `--library-path`; set
`"static": false` in `manifest.json` in that case. Treat it as a last resort and
repeat the glibc host test above before shipping it, since a wrapper that only
works inside the Alpine build image is worthless on a real host.

## Releasing

The `runtime-module` workflow runs on pull requests that touch `runtime/`. On
`ubuntu-latest` and `ubuntu-24.04-arm` (native, no emulation) it rebuilds the
archive, fails if it differs from the committed one, runs the Go tests, and
starts the unpacked `Xvnc` on the glibc runner to prove portability.

To release a new module version:

1. Change `build/Dockerfile` (for example bump `TIGERVNC_VERSION`), run
   `make runtime-module`, and commit the updated archives together with the
   change. CI confirms they match.
2. After merge, tag the nested module: `git tag runtime/vX.Y.Z && git push
   origin runtime/vX.Y.Z`. Go resolves `github.com/coder/portabledesktop/runtime@vX.Y.Z`
   from that tag.

## Bumping TigerVNC

1. Update `TIGERVNC_VERSION` and `TIGERVNC_SHA256` in `build/Dockerfile`.
2. Check the version of xorg-server that the release expects. TigerVNC's
   `BUILDING.txt` states the supported range and the `unix/xserver*.patch` files
   show which series it patches. Alpine's
   [tigervnc APKBUILD](https://gitlab.alpinelinux.org/alpine/aports/-/blob/master/community/tigervnc/APKBUILD)
   is a good reference for the exact xorg-server version and configure flags.
   Update `XORG_SERVER_VERSION` and `XORG_SERVER_SHA256` accordingly.
3. `make runtime-module`, commit, open a PR, then tag as described above.

The Alpine package versions used for the statically linked libraries are pinned
in `ARG APK_*_VERSION` and verified during the build. When a base image update
moves one of them, the build fails with the expected and actual version so that
the pin and the matching upstream license URL can be updated together.

## Measured sizes

Built from `alpine:3.22` with TigerVNC 1.16.0, xorg-server 21.1.22, xkbcomp
1.4.7 and xkeyboard-config 2.43:

| Target        | Compressed            | Unpacked              | `bin/Xvnc`  | `bin/xkbcomp` | `share/xkb` |
|---------------|-----------------------|-----------------------|-------------|---------------|-------------|
| `linux/amd64` | 1,793,051 B (1.7 MiB) | 4,689,003 B (4.5 MiB) | 2,761,256 B | 1,276,328 B   | 580 KiB     |
| `linux/arm64` | 1,737,086 B (1.7 MiB) | 4,752,763 B (4.5 MiB) | 2,772,888 B | 1,328,456 B   | 580 KiB     |
