package process

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gookit/goutil/x/assert"
)

func TestShortNamePrefixCleansDirectoryName(t *testing.T) {
	cases := []struct {
		name, dir, want string
	}{
		{"lower cases and keeps letters, digits and dashes", filepath.Join("tmp", "Suga-2"), "suga-2"},
		{"drops characters that need quoting", filepath.Join("tmp", "My Work_Tools.v2"), "myworktoolsv2"},
		{"keeps inner dashes", "my-tools-dev", "my-tools-dev"},
		{"a non-ascii directory falls back to suga", filepath.Join("tmp", "中文目录"), shortNameFallback},
		{"an empty directory falls back to suga", "", shortNameFallback},
		{"the file system root falls back to suga", string(filepath.Separator), shortNameFallback},
		{"truncates to 20 characters", strings.Repeat("a", 25), strings.Repeat("a", 20)},
		{"truncation drops a trailing dash", strings.Repeat("a", 19) + "-xy", strings.Repeat("a", 19)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Eq(t, c.want, ShortNamePrefix(c.dir))
		})
	}
}

func TestNewShortName(t *testing.T) {
	constant := func(suffix string) SuffixSource {
		return func() string { return suffix }
	}

	t.Run("directory part plus the suffix", func(t *testing.T) {
		assert.Eq(t, "mywork-k7", NewShortName(filepath.Join("tmp", "My Work"), nil, constant("k7")))
	})

	t.Run("retries while the name is in use", func(t *testing.T) {
		seq := []string{"aa", "bb", "cc"}
		calls := 0
		suffix := func() string { calls++; return seq[calls-1] }
		taken := func(name string) bool { return name != "ws-cc" }
		assert.Eq(t, "ws-cc", NewShortName("ws", taken, suffix))
		assert.Eq(t, 3, calls)
	})

	t.Run("a machine with every short name taken still gets a name", func(t *testing.T) {
		calls := 0
		suffix := func() string { calls++; return "aa" }
		got := NewShortName("ws", func(string) bool { return true }, suffix)
		assert.Eq(t, strings.Repeat("aa", 2), strings.TrimPrefix(got, "ws-"))
		assert.Eq(t, shortNameAttempts+2, calls)
	})

	t.Run("the same directory twice gives different names", func(t *testing.T) {
		seen := map[string]bool{}
		for i := 0; i < 50; i++ {
			seen[NewShortName("ws", nil, RandomSuffix)] = true
		}
		assert.True(t, len(seen) > 1)
	})
}

func TestRandomSuffix(t *testing.T) {
	t.Run("two characters of the short name alphabet", func(t *testing.T) {
		for i := 0; i < 50; i++ {
			got := RandomSuffix()
			assert.Eq(t, shortNameSuffixLen, len(got))
			assert.True(t, strings.Trim(got, shortNameAlphabet) == "", got)
		}
	})
}

func TestTakenNames(t *testing.T) {
	records := []Record{
		{PID: 7, Name: "ws-k7"},
		{PID: 8, Name: "other-aa"},
		{PID: 9, Name: "  Spaced-Bb  "},
		{PID: 10, Name: ""}, // a record from before short names
	}
	t.Run("ignores own record and nameless records", func(t *testing.T) {
		taken := TakenNames(records, 7)
		assert.Eq(t, false, taken("ws-k7"))
		assert.Eq(t, true, taken("other-aa"))
		assert.Eq(t, false, taken(""))
	})
	t.Run("compares without case and around spaces", func(t *testing.T) {
		taken := TakenNames(records, 7)
		assert.Eq(t, true, taken("OTHER-AA"))
		assert.Eq(t, true, taken(" spaced-bb "))
		assert.Eq(t, false, taken("ws-k8"))
	})
}

func TestResolvePrefersExactIDThenShortNameThenPrefix(t *testing.T) {
	// "ws" is the short name of one process and a prefix of both session IDs.
	records := []Record{
		{SessionID: "ws-1", Name: "one-k7", PID: 1},
		{SessionID: "ws-2", Name: "ws", PID: 2},
	}
	t.Run("exact session ID beats a short name", func(t *testing.T) {
		got, err := Resolve([]Record{{SessionID: "ab12", Name: "x-1", PID: 1}, {SessionID: "zz99", Name: "ab12", PID: 2}}, "ab12")
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, 1, got.PID)
	})
	t.Run("short name beats an ambiguous prefix", func(t *testing.T) {
		got, err := Resolve(records, "ws")
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, 2, got.PID)
	})
	t.Run("short name matches ignoring case", func(t *testing.T) {
		got, err := Resolve(records, "ONE-K7")
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, 1, got.PID)
	})
	t.Run("an ID prefix is the last resort", func(t *testing.T) {
		got, err := Resolve(records, "ws-2")
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, 2, got.PID)
	})
	t.Run("two processes with the same short name are ambiguous", func(t *testing.T) {
		_, err := Resolve([]Record{{SessionID: "s1", Name: "same-aa"}, {SessionID: "s2", Name: "same-aa"}}, "same-aa")
		assert.True(t, errors.Is(err, ErrAmbiguous))
	})
	t.Run("no match is offline", func(t *testing.T) {
		_, err := Resolve(records, "nope")
		assert.True(t, errors.Is(err, ErrNotOnline))
	})
	t.Run("an empty selector never matches a nameless record", func(t *testing.T) {
		_, err := Resolve([]Record{{SessionID: "s1"}}, " ")
		assert.True(t, errors.Is(err, ErrNotOnline))
	})
}
