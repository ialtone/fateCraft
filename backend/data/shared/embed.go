// Package shareddata embeds platform-owned content under data/shared.
package shareddata

import _ "embed"

//go:embed prompts/common.json
var CommonPromptJSON []byte
