package runtime

import _ "embed"

const archiveName = "desktop-runtime-linux-amd64.tar.zst"

//go:embed desktop-runtime-linux-amd64.tar.zst
var archive []byte
