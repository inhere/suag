package wiring

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	gkconfig "github.com/gookit/config/v2"

	"github.com/inhere/suag/internal/adapters/gofermem"
	axconfig "github.com/inhere/suag/internal/config"
	"github.com/inhere/suag/pkg/agentcore/memory"
	"github.com/inhere/suag/pkg/agentcore/memory/files"
)

// Memory backend names accepted by memory.<scope>.backend.
const (
	memoryBackendFiles = files.Name
	memoryBackendGofer = gofermem.Name
	memoryBackendExec  = "exec"
)

// Reasons recorded in MemoryBackendChoice.Reason (shown by the manifest).
const (
	MemoryReasonConfig       = "config"         // memory.<scope>.backend set explicitly
	MemoryReasonRepoIndex    = "repo_memory_md" // MEMORY.md exists at the repository root
	MemoryReasonGoferTracker = "gofer_tracker"  // .gofer/tracker exists at the repository root
	MemoryReasonDefault      = "default"        // built-in files backend under SUAG_HOME
)

// MemoryConfig is the memory part of the suag configuration.
type MemoryConfig struct {
	// UserBackend is memory.user.backend: "" or files (exec arrives with T14).
	UserBackend string
	// ProjectBackend is memory.project.backend: "" (auto), files, gofer or exec.
	ProjectBackend string
	// SuagHome is the absolute SUAG_HOME used for the built-in file layouts.
	SuagHome string
}

// loadMemoryConfig reads memory.*.backend from the loaded configuration.
func loadMemoryConfig() (MemoryConfig, error) {
	home, err := axconfig.ResolveSuagHome()
	if err != nil {
		return MemoryConfig{}, err
	}
	return MemoryConfig{
		UserBackend:    normBackend(gkconfig.String("memory.user.backend")),
		ProjectBackend: normBackend(gkconfig.String("memory.project.backend")),
		SuagHome:       home,
	}, nil
}

func normBackend(v string) string { return strings.ToLower(strings.TrimSpace(v)) }

// MemoryBackendChoice describes the backend serving one scope and why it was
// chosen.
type MemoryBackendChoice struct {
	Scope   memory.Scope
	Backend string // files or gofer
	Reason  string // MemoryReason*
	// Location is the files layout root (directory or repository root) or,
	// for gofer, the working directory gofer runs in.
	Location string
}

// MemorySetup is the memory router with the user and project scopes
// registered, plus the selection for each scope.
type MemorySetup struct {
	Router  *memory.Router
	User    MemoryBackendChoice
	Project MemoryBackendChoice
}

// buildMemory selects the user and project backends for workspaceRoot and
// registers them on a new Router. It runs no backend command.
func buildMemory(workspaceRoot string, cfg MemoryConfig) (*MemorySetup, error) {
	user, userChoice, err := selectUserBackend(cfg)
	if err != nil {
		return nil, err
	}
	project, projectChoice, err := selectProjectBackend(workspaceRoot, cfg)
	if err != nil {
		return nil, err
	}
	r := memory.NewRouter()
	if err := r.Register(memory.ScopeUser, user); err != nil {
		return nil, err
	}
	if err := r.Register(memory.ScopeProject, project); err != nil {
		return nil, err
	}
	return &MemorySetup{Router: r, User: userChoice, Project: projectChoice}, nil
}

// selectUserBackend returns the user-scope backend: files under
// <SUAG_HOME>/memory/user.
func selectUserBackend(cfg MemoryConfig) (memory.Store, MemoryBackendChoice, error) {
	choice := MemoryBackendChoice{Scope: memory.UserScope(), Backend: memoryBackendFiles, Reason: MemoryReasonDefault}
	switch cfg.UserBackend {
	case "":
	case memoryBackendFiles:
		choice.Reason = MemoryReasonConfig
	case memoryBackendExec:
		return nil, choice, errors.New("memory.user.backend: exec is not supported yet")
	case memoryBackendGofer:
		return nil, choice, errors.New("memory.user.backend: gofer only serves project memory; use files")
	default:
		return nil, choice, fmt.Errorf("memory.user.backend: unknown backend %q (want files)", cfg.UserBackend)
	}
	if err := requireHome(cfg); err != nil {
		return nil, choice, err
	}
	choice.Location = filepath.Join(cfg.SuagHome, "memory", "user")
	st, err := files.New(map[memory.Scope]files.Layout{choice.Scope: files.DirLayout{Dir: choice.Location}})
	return st, choice, err
}

// selectProjectBackend picks the project-scope backend (design "数据", Q1, Q2):
//
//  1. memory.project.backend when set (files still prefers the repository
//     MEMORY.md when one exists; exec is not supported yet);
//  2. MEMORY.md at the repository root → files in repository mode (the
//     file is never created by suag, only used when present);
//  3. .gofer/tracker at the repository root → gofer;
//  4. otherwise files under <SUAG_HOME>/projects/<key>/memory.
//
// The repository root is the Git root above workspaceRoot (workspaceRoot
// itself without one); <key> is derived from it by projectKey.
func selectProjectBackend(workspaceRoot string, cfg MemoryConfig) (memory.Store, MemoryBackendChoice, error) {
	var choice MemoryBackendChoice
	if strings.TrimSpace(workspaceRoot) == "" {
		return nil, choice, errors.New("memory: the workspace root is unknown")
	}
	ws, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return nil, choice, err
	}
	root, err := gitRoot(ws)
	if err != nil {
		return nil, choice, err
	}
	choice.Scope = memory.ProjectScope(projectKey(root))

	hasIndex, err := isFile(filepath.Join(root, "MEMORY.md"))
	if err != nil {
		return nil, choice, err
	}
	backend, reason := cfg.ProjectBackend, MemoryReasonConfig
	switch backend {
	case "":
		hasTracker, terr := isDir(filepath.Join(root, ".gofer", "tracker"))
		if terr != nil {
			return nil, choice, terr
		}
		switch {
		case hasIndex:
			backend, reason = memoryBackendFiles, MemoryReasonRepoIndex
		case hasTracker:
			backend, reason = memoryBackendGofer, MemoryReasonGoferTracker
		default:
			backend, reason = memoryBackendFiles, MemoryReasonDefault
		}
	case memoryBackendFiles, memoryBackendGofer:
	case memoryBackendExec:
		return nil, choice, errors.New("memory.project.backend: exec is not supported yet")
	default:
		return nil, choice, fmt.Errorf("memory.project.backend: unknown backend %q (want files, gofer or exec)", backend)
	}
	choice.Backend, choice.Reason = backend, reason

	if backend == memoryBackendGofer {
		choice.Location = root
		st, gerr := gofermem.New(gofermem.Config{Dir: root})
		return st, choice, gerr
	}
	var layout files.Layout
	if hasIndex {
		choice.Location = root
		layout = files.RepoLayout{Root: root}
	} else {
		if err := requireHome(cfg); err != nil {
			return nil, choice, err
		}
		choice.Location = filepath.Join(cfg.SuagHome, "projects", choice.Scope.Key, "memory")
		layout = files.DirLayout{Dir: choice.Location}
	}
	st, err := files.New(map[memory.Scope]files.Layout{choice.Scope: layout})
	return st, choice, err
}

func requireHome(cfg MemoryConfig) error {
	if cfg.SuagHome == "" || !filepath.IsAbs(cfg.SuagHome) {
		return fmt.Errorf("memory: SUAG_HOME must be an absolute path, got %q", cfg.SuagHome)
	}
	return nil
}

// gitRoot returns the nearest directory at or above dir that contains .git
// (a directory or a worktree file), or dir itself when there is none. It
// matches the workspace root used for project rules.
func gitRoot(dir string) (string, error) {
	for cur := dir; ; cur = filepath.Dir(cur) {
		if _, err := os.Stat(filepath.Join(cur, ".git")); err == nil {
			return cur, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		if filepath.Dir(cur) == cur {
			return dir, nil
		}
	}
}

// projectKeySlugLen bounds the readable part of a project key; the tail of
// the path is kept because it names the project.
const projectKeySlugLen = 48

// projectKey turns an absolute repository path into a readable, file-name
// safe key: lowercase ASCII letters and digits joined by '-', followed by a
// short hash of the full path so different paths with the same slug (or with
// non-ASCII names) do not collide. Windows paths are hashed case-insensitively.
func projectKey(root string) string {
	p := filepath.ToSlash(filepath.Clean(root))
	if runtime.GOOS == "windows" {
		p = strings.ToLower(p)
	}
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(p) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r)
			dash = false
			continue
		}
		dash = true
	}
	slug := b.String()
	if len(slug) > projectKeySlugLen {
		slug = strings.TrimLeft(slug[len(slug)-projectKeySlugLen:], "-")
	}
	sum := sha256.Sum256([]byte(p))
	h := hex.EncodeToString(sum[:4])
	if slug == "" {
		return h
	}
	return slug + "-" + h
}

func isFile(path string) (bool, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return info.Mode().IsRegular(), nil
}

func isDir(path string) (bool, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return info.IsDir(), nil
}
