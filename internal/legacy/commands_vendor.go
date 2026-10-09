//go:build vendor

package legacy

import _ "embed"

// commandIndex is the legacy CLI's "list --all --format=json" output, generated at build time with the embedded config.
//
//go:embed archives/commands-vendor.json
var commandIndex []byte
