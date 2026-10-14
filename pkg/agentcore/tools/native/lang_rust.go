package native

import (
	"os"
	"path/filepath"
)

// rustLanguage is the Rust declaration: the crate sources under CARGO_HOME as a
// read-only root (= ~/.cargo/registry/src, which holds the unpacked crates; the
// tool home itself is not handed out, it holds credentials too) and `cargo
// test` (with `--test` for one test binary) to run the tests.
var rustLanguage = Language{
	Name:         "Rust",
	Markers:      []string{"Cargo.toml", "Cargo.lock"},
	TestCommands: `\bcargo test\b`,
	FocusedFlags: []string{"--test"},
	dependencyRoots: func(env langEnv, add *rootCollector) {
		cargoHome := os.Getenv("CARGO_HOME")
		if cargoHome == "" && env.home != "" {
			cargoHome = filepath.Join(env.home, ".cargo")
		}
		if cargoHome != "" {
			add.add("Rust crates", filepath.Join(cargoHome, "registry", "src"))
		}
	},
}
