package memory

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/gookit/goutil/x/assert"
)

func TestKind(t *testing.T) {
	t.Run("valid kinds", func(t *testing.T) {
		for _, k := range Kinds() {
			assert.True(t, k.Valid())
		}
		assert.False(t, Kind("note").Valid())
		assert.False(t, Kind("").Valid())
	})
	t.Run("parse", func(t *testing.T) {
		k, err := ParseKind(" Feedback ")
		assert.NoErr(t, err)
		assert.Eq(t, KindFeedback, k)
		_, err = ParseKind("note")
		assert.True(t, errors.Is(err, ErrInvalid))
	})
}

func TestScope(t *testing.T) {
	t.Run("string and parse round trip", func(t *testing.T) {
		for _, s := range []Scope{UserScope(), ProjectScope("suga"), {Kind: ScopePeople, Key: "alice"}, ProjectScope("a:b")} {
			got, err := ParseScope(s.String())
			assert.NoErr(t, err)
			assert.Eq(t, s, got)
		}
		assert.Eq(t, "user", UserScope().String())
		assert.Eq(t, "project:suga", ProjectScope("suga").String())
	})
	t.Run("invalid", func(t *testing.T) {
		for _, v := range []string{"", "team", "project", "project:", "people: ", "user:x", "project:a\nb"} {
			_, err := ParseScope(v)
			assert.True(t, errors.Is(err, ErrInvalid), v)
		}
	})
}

func TestValidateID(t *testing.T) {
	t.Run("accepted", func(t *testing.T) {
		for _, id := range []string{"a", "go-tips", "x1-2", "a1b2c3d4", strings.Repeat("a", MaxIDLen)} {
			assert.NoErr(t, ValidateID(id), id)
		}
	})
	t.Run("rejected", func(t *testing.T) {
		bad := []string{"", "../x", "a/b", `a\b`, "/etc/passwd", `C:\x`, "C:x", ".", "..", ".hidden",
			"a.md", "Go", "-a", "a-", "a b", "memory", "中文", strings.Repeat("a", MaxIDLen+1)}
		for _, id := range bad {
			err := ValidateID(id)
			assert.True(t, errors.Is(err, ErrInvalid), id)
		}
	})
}

func TestSlug(t *testing.T) {
	t.Run("ascii", func(t *testing.T) {
		assert.Eq(t, "prefer-table-tests", Slug("Prefer  table-tests!"))
		assert.Eq(t, "go-1-26", Slug("  Go 1.26 "))
	})
	t.Run("non ascii gets a stable hash", func(t *testing.T) {
		zh := Slug("测试规范")
		assert.NoErr(t, ValidateID(zh))
		assert.Len(t, zh, 8)
		assert.Eq(t, zh, Slug("测试规范"))
		assert.NotEq(t, zh, Slug("提交规范"))

		mixed := Slug("Go 测试")
		assert.True(t, strings.HasPrefix(mixed, "go-"), mixed)
		assert.NoErr(t, ValidateID(mixed))
	})
	t.Run("long titles stay valid", func(t *testing.T) {
		s := Slug(strings.Repeat("word ", 40) + "中")
		assert.NoErr(t, ValidateID(s))
	})
	t.Run("punctuation only falls back to hash", func(t *testing.T) {
		assert.NoErr(t, ValidateID(Slug("!!!")))
	})
}

func TestUniqueID(t *testing.T) {
	taken := map[string]bool{"go-tips": true, "go-tips-2": true}
	isTaken := func(id string) bool { return taken[id] }

	t.Run("collisions get a sequence", func(t *testing.T) {
		assert.Eq(t, "go-tips-3", UniqueID("go-tips", isTaken))
		assert.Eq(t, "fresh", UniqueID("fresh", isTaken))
	})
	t.Run("reserved id", func(t *testing.T) {
		assert.Eq(t, "memory-2", UniqueID(Slug("Memory"), isTaken))
	})
	t.Run("suffix fits the length limit", func(t *testing.T) {
		long := strings.Repeat("a", MaxIDLen)
		id := UniqueID(long, func(id string) bool { return id == long })
		assert.NoErr(t, ValidateID(id))
		assert.True(t, strings.HasSuffix(id, "-2"), id)
	})
}

func TestFingerprint(t *testing.T) {
	a := Fingerprint("Prefer Tables", "use  table\ntests")
	assert.Eq(t, a, Fingerprint(" prefer tables ", "USE table tests"))
	assert.NotEq(t, a, Fingerprint("prefer tables", "use tables"))
	assert.Len(t, a, 64)
}

func TestOpValidate(t *testing.T) {
	ok := []Op{
		{Action: ActionAdd, Title: "t", Body: "b", Kind: KindUser, AppliesWhen: []string{"Go 1.26", "Windows"}},
		{Action: ActionReplace, ID: "t", Title: "t", Body: "b"},
		{Action: ActionRemove, ID: "t"},
	}
	for _, op := range ok {
		assert.NoErr(t, op.Validate(), string(op.Action))
	}
	bad := map[string]Op{
		"add with id":            {Action: ActionAdd, ID: "x", Title: "t", Body: "b", Kind: KindUser},
		"add no kind":            {Action: ActionAdd, Title: "t", Body: "b"},
		"add empty body":         {Action: ActionAdd, Title: "t", Body: " ", Kind: KindUser},
		"multiline title":        {Action: ActionAdd, Title: "a\nb", Body: "b", Kind: KindUser},
		"replace bad id":         {Action: ActionReplace, ID: "../x", Title: "t", Body: "b"},
		"replace bad kind":       {Action: ActionReplace, ID: "x", Title: "t", Body: "b", Kind: "note"},
		"remove missing id":      {Action: ActionRemove},
		"unknown action":         {Action: "upsert", ID: "x"},
		"too many applies_when":  {Action: ActionAdd, Title: "t", Body: "b", Kind: KindUser, AppliesWhen: make([]string, 11)},
		"multiline applies_when": {Action: ActionAdd, Title: "t", Body: "b", Kind: KindUser, AppliesWhen: []string{"Windows\nLinux"}},
		"long applies_when":      {Action: ActionAdd, Title: "t", Body: "b", Kind: KindUser, AppliesWhen: []string{strings.Repeat("中", 201)}},
	}
	for name, op := range bad {
		t.Run(name, func(t *testing.T) {
			assert.True(t, errors.Is(op.Validate(), ErrInvalid))
		})
	}
}

func TestIndexRender(t *testing.T) {
	assert.Eq(t, "", Index{Scope: UserScope()}.Render())
	ix := Index{Scope: ProjectScope("suga"), Lines: []IndexLine{
		{ID: "table-tests", Title: "Prefer table tests", Kind: KindFeedback},
		{ID: "ci", Title: "CI runs on host", Kind: KindProject},
	}}
	want := "### Memory: project:suga\n- table-tests (feedback): Prefer table tests\n- ci (project): CI runs on host\n"
	assert.Eq(t, want, ix.Render())
	assert.Eq(t, len(want), ix.size())
}

// TestDependencies keeps memory and the built-in files backend on the
// standard library plus agentcore/redact and agentcore/api (plan T11
// completion criterion). Other backends (exec) are checked by their owners.
func TestDependencies(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("go command not found: %v", err)
	}
	cmd := exec.Command(goBin, "list", "-deps", "-f", "{{if not .Standard}}{{.ImportPath}}{{end}}", ".", "./files")
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=readonly")
	out, err := cmd.Output()
	assert.Require(t, assert.NoErr(t, err))
	allowed := []string{
		"github.com/inhere/suga/pkg/agentcore/memory",
		"github.com/inhere/suga/pkg/agentcore/memory/files",
		"github.com/inhere/suga/pkg/agentcore/redact",
		"github.com/inhere/suga/pkg/agentcore/api",
	}
	for _, path := range strings.Fields(string(out)) {
		ok := false
		for _, a := range allowed {
			if path == a {
				ok = true
				break
			}
		}
		assert.True(t, ok, "memory must not depend on "+path)
	}
}
