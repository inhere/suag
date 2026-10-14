//go:build windows

package wiring

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gookit/goutil/x/assert"
)

// readerHold is how long the reader of the released directory keeps its handle
// open: long enough that the release really meets it, and well inside the
// release's retry window (asideRetries * asideRetryDelay).
const readerHold = 150 * time.Millisecond

// TestReleaseBuiltinSkillsWaitsForAReader: Windows refuses to rename a released
// skill directory while another process reads a file inside it ("Access is
// denied"), which is what a second suga starting at the same moment does when
// it reads the released marker. The release waits for the reader and finishes
// the upgrade instead of failing (suga-se0o).
func TestReleaseBuiltinSkillsWaitsForAReader(t *testing.T) {
	home := t.TempDir()
	assert.NoErr(t, releaseBuiltinSkills(home, "1.0.0", guideFS("one")))
	dir := releasedDir(home)

	// A reader holding the released SKILL.md open, as another release's marker
	// read does. It closes the handle by itself, so a failed test leaks none.
	reader, err := os.Open(filepath.Join(dir, "SKILL.md"))
	assert.Require(t, assert.NoErr(t, err))
	go func() {
		defer reader.Close()
		time.Sleep(readerHold)
	}()

	// The upgrade replaces the directory, so it has to move the old one aside
	// first: without the wait that rename fails while the reader holds it open.
	assert.NoErr(t, releaseBuiltinSkills(home, "2.0.0", guideFS("two")))
	assert.StrContains(t, mustReadFile(t, filepath.Join(dir, "SKILL.md")), "two")
	assert.Eq(t, "2.0.0", mustReadMarker(t, dir).Version)
}
