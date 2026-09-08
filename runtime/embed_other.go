//go:build !linux || !(amd64 || arm64)

package runtime

// No runtime is built for this platform.
const archiveName = ""

var archive []byte
