package config

import "embed"

// Files contains the onboarding templates embedded into the suga binary.
// Keeping the source files beside the full reference configuration makes the
// generated first-run files reviewable without reading the Go implementation.
//
//go:embed onboarding.yaml onboarding.env
var Files embed.FS
