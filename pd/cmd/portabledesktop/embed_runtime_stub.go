//go:build !embed_runtime

package main

// embeddedRuntime is nil when building without the embedded runtime.
// The CLI will require PORTABLEDESKTOP_RUNTIME_DIR to be set.
var embeddedRuntime []byte
