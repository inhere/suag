// Package skills embeds suga's built-in skills. They are released into
// <SUGA_HOME>/skills on start-up (see internal/wiring/builtin_skills.go) and
// loaded from there like any other skill, so a user can read, adapt or replace
// them with plain files.
package skills

import "embed"

// FS holds the built-in skill directories, one directory per skill
// (suga-guide/SKILL.md and its references). "all:" also includes files whose
// name starts with "_" or ".".
//
//go:embed all:suga-guide
var FS embed.FS
