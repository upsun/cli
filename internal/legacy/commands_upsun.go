//go:build !platformsh && !vendor

package legacy

import _ "embed"

// commandIndex is the legacy CLI's "list --all --format=json" output, generated at build time with the embedded config.
//
//go:embed archives/commands-upsun.json
var commandIndex []byte
