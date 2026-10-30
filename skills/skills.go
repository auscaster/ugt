// Package skills holds UGT's agent skill, built into ugt so that connecting
// an AI app that reads skills, such as Claude Code or Codex, installs it with
// the MCP server.
package skills

import "embed"

// FS holds the skill as ugt/SKILL.md and its references.
//
//go:embed all:ugt
var FS embed.FS
