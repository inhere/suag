package wiring

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	gkconfig "github.com/gookit/config/v2"
	"github.com/gookit/config/v2/yaml"
	"github.com/gookit/goutil/x/assert"

	"github.com/inhere/suag/internal/adapters/gofermem"
	"github.com/inhere/suag/pkg/agentcore/memory"
)

// memRepo builds a fake repository: <tmp>/repo with .git, optionally a root
// MEMORY.md and a .gofer/tracker directory, plus a nested workspace dir.
func memRepo(t *testing.T, withIndex, withTracker bool) (root, sub string) {
	t.Helper()
	root = filepath.Join(t.TempDir(), "My Repo")
	sub = filepath.Join(root, "pkg", "inner")
	assert.Require(t, assert.NoErr(t, os.MkdirAll(filepath.Join(root, ".git"), 0o700)))
	assert.Require(t, assert.NoErr(t, os.MkdirAll(sub, 0o700)))
	if withIndex {
		assert.Require(t, assert.NoErr(t, os.WriteFile(filepath.Join(root, "MEMORY.md"), []byte("# Notes\n"), 0o600)))
	}
	if withTracker {
		assert.Require(t, assert.NoErr(t, os.MkdirAll(filepath.Join(root, ".gofer", "tracker"), 0o700)))
	}
	return root, sub
}

func TestSelectProjectBackend(t *testing.T) {
	home := t.TempDir()
	for _, tc := range []struct {
		name        string
		index       bool
		tracker     bool
		configured  string
		wantBackend string
		wantReason  string
		wantRepo    bool // files in repository mode (location = repo root)
	}{
		{name: "repo MEMORY.md", index: true, wantBackend: "files", wantReason: MemoryReasonRepoIndex, wantRepo: true},
		{name: "MEMORY.md wins over gofer tracker", index: true, tracker: true, wantBackend: "files", wantReason: MemoryReasonRepoIndex, wantRepo: true},
		{name: "gofer tracker", tracker: true, wantBackend: "gofer", wantReason: MemoryReasonGoferTracker},
		{name: "default files under SUAG_HOME", wantBackend: "files", wantReason: MemoryReasonDefault},
		{name: "config files overrides tracker", tracker: true, configured: "files", wantBackend: "files", wantReason: MemoryReasonConfig},
		{name: "config files keeps repo MEMORY.md", index: true, tracker: true, configured: "files", wantBackend: "files", wantReason: MemoryReasonConfig, wantRepo: true},
		{name: "config gofer overrides MEMORY.md", index: true, configured: "gofer", wantBackend: "gofer", wantReason: MemoryReasonConfig},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, sub := memRepo(t, tc.index, tc.tracker)
			st, choice, err := selectProjectBackend(sub, MemoryConfig{ProjectBackend: tc.configured, SuagHome: home})
			assert.Require(t, assert.NoErr(t, err))
			assert.Eq(t, tc.wantBackend, choice.Backend)
			assert.Eq(t, tc.wantReason, choice.Reason)
			assert.Eq(t, tc.wantBackend, memory.BackendName(st))
			assert.Eq(t, memory.ProjectScope(projectKey(root)), choice.Scope)

			switch {
			case tc.wantBackend == "gofer":
				_, ok := st.(*gofermem.Store)
				assert.True(t, ok)
				assert.Eq(t, root, choice.Location, "gofer runs in the repository root")
			case tc.wantRepo:
				assert.Eq(t, root, choice.Location)
			default:
				assert.Eq(t, filepath.Join(home, "projects", choice.Scope.Key, "memory"), choice.Location)
			}

			// Files backends really write where the choice says.
			if tc.wantBackend == "files" {
				e, err := st.Write(context.Background(), choice.Scope, memory.Op{Action: memory.ActionAdd,
					Title: "Probe", Body: "where am I", Kind: memory.KindProject})
				assert.Require(t, assert.NoErr(t, err))
				entry := filepath.Join(choice.Location, e.ID+".md")
				if tc.wantRepo {
					entry = filepath.Join(root, ".suag", "memory", e.ID+".md")
				}
				_, serr := os.Stat(entry)
				assert.NoErr(t, serr)
			}
		})
	}

	t.Run("errors", func(t *testing.T) {
		_, sub := memRepo(t, false, true)
		for _, tc := range []struct{ name, configured, want string }{
			{"exec without command", "exec", "memory.project.command is not set"},
			{"unknown backend", "sqlite", "unknown backend"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				_, _, err := selectProjectBackend(sub, MemoryConfig{ProjectBackend: tc.configured, SuagHome: home})
				assert.Err(t, err)
				assert.StrContains(t, err.Error(), tc.want)
			})
		}
		_, _, err := selectProjectBackend("", MemoryConfig{SuagHome: home})
		assert.Err(t, err)
		_, _, err = selectProjectBackend(sub, MemoryConfig{ProjectBackend: "files"})
		assert.Err(t, err, "the SUAG_HOME layout needs an absolute home")
	})

	t.Run("MEMORY.md directory and tracker file are ignored", func(t *testing.T) {
		root, sub := memRepo(t, false, false)
		assert.NoErr(t, os.MkdirAll(filepath.Join(root, "MEMORY.md"), 0o700))
		assert.NoErr(t, os.MkdirAll(filepath.Join(root, ".gofer"), 0o700))
		assert.NoErr(t, os.WriteFile(filepath.Join(root, ".gofer", "tracker"), nil, 0o600))
		_, choice, err := selectProjectBackend(sub, MemoryConfig{SuagHome: home})
		assert.NoErr(t, err)
		assert.Eq(t, MemoryReasonDefault, choice.Reason)
	})

	t.Run("no git root uses the workspace itself", func(t *testing.T) {
		ws := t.TempDir()
		assert.NoErr(t, os.WriteFile(filepath.Join(ws, "MEMORY.md"), nil, 0o600))
		_, choice, err := selectProjectBackend(ws, MemoryConfig{SuagHome: home})
		assert.NoErr(t, err)
		assert.Eq(t, MemoryReasonRepoIndex, choice.Reason)
		assert.Eq(t, ws, choice.Location)
	})
}

func TestProjectKey(t *testing.T) {
	keyPattern := regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*-[0-9a-f]{8}$`)
	t.Run("readable slug plus hash", func(t *testing.T) {
		k := projectKey("/D/Work/inhere-tools/Suag_Dev")
		assert.True(t, keyPattern.MatchString(k), k)
		assert.StrContains(t, k, "d-work-inhere-tools-suag-dev-")
		assert.Eq(t, k, projectKey("/D/Work/inhere-tools/Suag_Dev/"), "stable after Clean")
		assert.NoErr(t, memory.ProjectScope(k).Validate())
	})
	t.Run("same slug different paths", func(t *testing.T) {
		assert.NotEq(t, projectKey("/a/b-c"), projectKey("/a/b/c"))
	})
	t.Run("long and non-ASCII paths", func(t *testing.T) {
		long := projectKey("/x/very/deeply/nested/directory/structure/for/the/project/named/suag-agent")
		assert.True(t, keyPattern.MatchString(long), long)
		assert.True(t, len(long) <= projectKeySlugLen+9)
		assert.StrContains(t, long, "suag-agent-")
		cn := projectKey("/项目/记忆")
		assert.True(t, regexp.MustCompile(`^[0-9a-f]{8}$`).MatchString(cn), cn)
	})
}

func TestBuildMemory(t *testing.T) {
	home := t.TempDir()
	_, sub := memRepo(t, false, true)

	t.Run("user files and project gofer", func(t *testing.T) {
		setup, err := buildMemory(sub, MemoryConfig{SuagHome: home})
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, []memory.RouteInfo{{Scope: "project", Backend: "gofer"}, {Scope: "user", Backend: "files"}}, setup.Router.Backends())
		assert.Eq(t, MemoryBackendChoice{Scope: memory.UserScope(), Backend: "files", Reason: MemoryReasonDefault,
			Location: filepath.Join(home, "memory", "user")}, setup.User)
		assert.Eq(t, MemoryReasonGoferTracker, setup.Project.Reason)

		// The user scope writes under SUAG_HOME through the router.
		e, err := setup.Router.Write(context.Background(), memory.UserScope(), memory.Op{Action: memory.ActionAdd,
			Title: "Prefers Chinese replies", Body: "reply in Chinese", Kind: memory.KindUser})
		assert.Require(t, assert.NoErr(t, err))
		_, serr := os.Stat(filepath.Join(home, "memory", "user", e.ID+".md"))
		assert.NoErr(t, serr)
	})
	t.Run("user backend config", func(t *testing.T) {
		setup, err := buildMemory(sub, MemoryConfig{UserBackend: "files", SuagHome: home})
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, MemoryReasonConfig, setup.User.Reason)
		for _, bad := range []string{"gofer", "exec", "nope"} {
			_, err := buildMemory(sub, MemoryConfig{UserBackend: bad, SuagHome: home})
			assert.Err(t, err, bad)
		}
	})
	t.Run("project error propagates", func(t *testing.T) {
		_, err := buildMemory(sub, MemoryConfig{ProjectBackend: "exec", SuagHome: home})
		assert.Err(t, err)
	})
}

func TestMemoryExecBackend(t *testing.T) {
	home := t.TempDir()
	_, sub := memRepo(t, true, true) // MEMORY.md and tracker present: config still wins

	t.Run("project exec choice", func(t *testing.T) {
		st, choice, err := selectProjectBackend(sub, MemoryConfig{
			ProjectBackend: "exec", SuagHome: home,
			ProjectExec: MemoryExecConfig{Command: "/opt/mem-backend", Args: []string{"--x"}, TimeoutSec: 3},
		})
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, "exec", choice.Backend)
		assert.Eq(t, MemoryReasonConfig, choice.Reason)
		assert.Eq(t, "/opt/mem-backend", choice.Location)
		assert.Eq(t, "exec", memory.BackendName(st))
	})
	t.Run("user exec choice", func(t *testing.T) {
		st, choice, err := selectUserBackend(MemoryConfig{
			UserBackend: "exec", SuagHome: home, UserExec: MemoryExecConfig{Command: "mem-user"},
		})
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, "exec", choice.Backend)
		assert.Eq(t, "mem-user", choice.Location)
		assert.Eq(t, "exec", memory.BackendName(st))
	})
	t.Run("missing command is a clear error", func(t *testing.T) {
		_, _, err := selectUserBackend(MemoryConfig{UserBackend: "exec", SuagHome: home})
		assert.Err(t, err)
		assert.StrContains(t, err.Error(), "memory.user.command is not set")
		// A command configured for the other scope is not borrowed.
		_, _, err = selectProjectBackend(sub, MemoryConfig{ProjectBackend: "exec", SuagHome: home,
			UserExec: MemoryExecConfig{Command: "mem-user"}})
		assert.Err(t, err)
		assert.StrContains(t, err.Error(), "memory.project.command is not set")
	})
}

func TestLoadMemoryConfig(t *testing.T) {
	gkconfig.AddDriver(yaml.Driver)
	resetConfig(t)
	cfg, err := loadMemoryConfig()
	assert.Require(t, assert.NoErr(t, err))
	assert.Eq(t, "", cfg.ProjectBackend)
	assert.Eq(t, "", cfg.UserBackend)
	assert.True(t, filepath.IsAbs(cfg.SuagHome))

	assert.Require(t, assert.NoErr(t, gkconfig.LoadStrings(gkconfig.Yaml, "memory:\n  user:\n    backend: Files\n  project:\n    backend: ' gofer '\n")))
	cfg, err = loadMemoryConfig()
	assert.Require(t, assert.NoErr(t, err))
	assert.Eq(t, "files", cfg.UserBackend)
	assert.Eq(t, "gofer", cfg.ProjectBackend)
}

func TestLoadMemoryExecConfig(t *testing.T) {
	gkconfig.AddDriver(yaml.Driver)
	resetConfig(t)
	yml := "memory:\n  user:\n    backend: exec\n    command: ' /bin/mem '\n    args: [--a, b]\n    timeout_sec: 7\n  project:\n    backend: exec\n    command: pm\n"
	assert.Require(t, assert.NoErr(t, gkconfig.LoadStrings(gkconfig.Yaml, yml)))
	cfg, err := loadMemoryConfig()
	assert.Require(t, assert.NoErr(t, err))
	assert.Eq(t, "exec", cfg.UserBackend)
	assert.Eq(t, MemoryExecConfig{Command: "/bin/mem", Args: []string{"--a", "b"}, TimeoutSec: 7}, cfg.UserExec)
	assert.Eq(t, MemoryExecConfig{Command: "pm"}, cfg.ProjectExec)

	t.Run("bad types", func(t *testing.T) {
		for _, bad := range []string{
			"memory:\n  user:\n    command: [a]\n",
			"memory:\n  user:\n    args: nope\n",
			"memory:\n  user:\n    args: [1]\n",
			"memory:\n  user:\n    timeout_sec: 0\n",
		} {
			resetConfig(t)
			assert.Require(t, assert.NoErr(t, gkconfig.LoadStrings(gkconfig.Yaml, bad)))
			_, err := loadMemoryConfig()
			assert.Err(t, err, bad)
		}
	})
}
