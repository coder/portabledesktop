// Package runtime embeds the minimal portabledesktop runtime: a fully static
// TigerVNC Xvnc, xkbcomp and a trimmed xkeyboard-config data set, packed as a
// zstd compressed tar archive per Linux architecture.
//
// The module exists so that Go programs can ship a working X server without
// downloading anything at run time. Unpack the archive to a directory you own
// and exec bin/Xvnc with -xkbdir <dir>/share/xkb and <dir>/bin on PATH; see
// build/README.md for the full launch contract.
//
// Archives are only present for linux/amd64 and linux/arm64. On every other
// GOOS/GOARCH Available reports false and Archive returns nil.
package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
)

// XvncPath is the path of the Xvnc executable relative to an unpacked archive.
const XvncPath = "bin/Xvnc"

// XKBDir is the path of the keymap data relative to an unpacked archive. Pass
// it to Xvnc as -xkbdir.
const XKBDir = "share/xkb"

// ArchiveName is the file name of the embedded archive for the current
// GOOS/GOARCH, or empty when none is embedded.
const ArchiveName = archiveName

// Available reports whether an archive is embedded for this GOOS/GOARCH.
func Available() bool {
	return len(archive) > 0
}

// Archive returns the zstd compressed tar archive. The returned slice must
// not be modified. It is nil when Available is false.
func Archive() []byte {
	return archive
}

var (
	sumOnce sync.Once
	sum     string
)

// SHA256 returns the hex encoded SHA-256 of the embedded archive, suitable
// for keying an unpack cache so that a new module version never reuses a
// stale runtime. It is empty when Available is false.
func SHA256() string {
	sumOnce.Do(func() {
		if len(archive) == 0 {
			return
		}
		h := sha256.Sum256(archive)
		sum = hex.EncodeToString(h[:])
	})
	return sum
}
