package main

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// RepoSpec selects repository mode: the workspace is `git archive <Ref>` of the
// git repository containing the eval's working directory.
type RepoSpec struct {
	Ref string `yaml:"ref"`
}

// prepareWorkspace builds ws: a fixture copy plus `git init`, or in repository
// mode a clean one-commit git repo holding the ref's tree. Generate entries
// are written before the base commit in both modes.
func (t *Task) prepareWorkspace(ctx context.Context, ws string) error {
	if t.Repo == nil {
		if err := copyDir(filepath.Join(t.dir, "fixture"), ws); err != nil {
			return fmt.Errorf("copy fixture: %w", err)
		}
	} else if err := archiveRef(ctx, ".", t.Repo.Ref, ws); err != nil {
		return fmt.Errorf("repo %s: %w", t.Repo.Ref, err)
	}
	for _, g := range t.Generate {
		if err := g.write(ws); err != nil {
			return fmt.Errorf("generate: %w", err)
		}
	}
	if out, err := exec.CommandContext(ctx, "git", "-C", ws, "init", "-q").CombinedOutput(); err != nil {
		return fmt.Errorf("git init: %v: %s", err, out)
	}
	if t.Repo == nil {
		return nil
	}
	// The identity is repo-local: suag runs with an isolated HOME (no global
	// gitconfig) while omp keeps the real one, so without it only suag's
	// commits failed and cost extra steps.
	for _, args := range [][]string{
		{"config", "user.name", "eval"},
		{"config", "user.email", "eval@local"},
		{"config", "commit.gpgsign", "false"},
		{"add", "-A"},
		{"commit", "-q", "-m", "base"},
	} {
		if out, err := exec.CommandContext(ctx, "git", append([]string{"-C", ws}, args...)...).CombinedOutput(); err != nil {
			return fmt.Errorf("git %s: %v: %s", args[len(args)-1], err, out)
		}
	}
	return nil
}

// archiveRef runs `git archive <ref>` in repoDir (exec, no shell) and unpacks
// the tar stream into dst with archive/tar, so no tar command is needed.
func archiveRef(ctx context.Context, repoDir, ref, dst string) error {
	if strings.HasPrefix(ref, "-") {
		return errors.New("ref must not start with '-'")
	}
	cmd := exec.CommandContext(ctx, "git", "-C", repoDir, "archive", "--format=tar", ref)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	exErr := extractTar(stdout, dst)
	_, _ = io.Copy(io.Discard, stdout)
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("git archive: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	return exErr
}

// extractTar unpacks r under dst; entries escaping dst are rejected.
// Symlinks are best effort (Windows may refuse them).
func extractTar(r io.Reader, dst string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	tr := tar.NewReader(r)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if h.Typeflag == tar.TypeXGlobalHeader {
			continue
		}
		p, err := safeJoin(dst, h.Name)
		if err != nil {
			return err
		}
		switch h.Typeflag {
		case tar.TypeDir:
			err = os.MkdirAll(p, 0o755)
		case tar.TypeReg:
			if err = os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				return err
			}
			mode := os.FileMode(0o644)
			if h.FileInfo().Mode()&0o111 != 0 {
				mode = 0o755
			}
			var f *os.File
			if f, err = os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode); err != nil {
				return err
			}
			_, err = io.Copy(f, tr)
			if cerr := f.Close(); err == nil {
				err = cerr
			}
		case tar.TypeSymlink:
			if err = os.MkdirAll(filepath.Dir(p), 0o755); err == nil {
				_ = os.Symlink(h.Linkname, p)
			}
		}
		if err != nil {
			return err
		}
	}
}

// splitArgs splits an extra-arguments flag value on whitespace.
func splitArgs(s string) []string { return strings.Fields(s) }
