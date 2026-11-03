// Package assets holds UGT's brand images for the builds that ship them.
package assets

import _ "embed"

// Icon is UGT's icon, the red tile, 1024 pixels square: the plugins' icon and
// the Claude Desktop bundle's.
//
//go:embed ugt-icon.png
var Icon []byte
