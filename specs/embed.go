package specs

import "embed"

// Files contains the built-in Windows command specifications.
//
//go:embed *.json
var Files embed.FS
