//go:build embed_runtime

package main

import _ "embed"

//go:embed runtime.tar.zst
var embeddedRuntime []byte
