package viewer

import (
	_ "embed"
)

// viewerClientJS is the noVNC client JavaScript bundle, built by:
//
//	bun build src/bin/viewer-client.entry.ts \
//	  --outfile go/internal/viewer/viewer-client.js \
//	  --target=browser --format=esm
//
//go:embed viewer-client.js
var viewerClientJS string
