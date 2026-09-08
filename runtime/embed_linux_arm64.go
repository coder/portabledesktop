package runtime

import _ "embed"

const archiveName = "desktop-runtime-linux-arm64.tar.zst"

//go:embed desktop-runtime-linux-arm64.tar.zst
var archive []byte
