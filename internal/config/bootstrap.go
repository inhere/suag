package axconfig

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	onboarding "github.com/inhere/suga/config"
)

// BootstrapOptions controls the safe, first-run initialization operation.
// Force only affects the home config.yaml; the .env file is never overwritten.
type BootstrapOptions struct {
	Force bool
}

// BootstrapResult describes files considered by EnsureHome and the files it
// created. It is intentionally a data-only result so command output stays in
// the CLI layer and initialization remains useful to non-CLI callers.
type BootstrapResult struct {
	Home          string
	Config        string
	Env           string
	Source        string
	CreatedHome   bool
	CreatedConfig bool
	CreatedEnv    bool
	Backup        string
	Force         bool
}

// BootstrapError carries the user-facing exit code for initialization policy
// errors, while ordinary filesystem failures continue to use exit code 1.
type BootstrapError struct {
	Code int
	Err  error
}

func (e BootstrapError) Error() string { return e.Err.Error() }
func (e BootstrapError) Unwrap() error { return e.Err }
func (e BootstrapError) ExitCode() int { return e.Code }

// EnsureHome creates the private suga root and safe onboarding files. The
// filesystem lock and the second existence check are deliberately inside the
// same owner: concurrent first runs can never overwrite a user's files.
func EnsureHome(opts BootstrapOptions) (BootstrapResult, error) {
	home, err := ResolveSugaHome()
	if err != nil {
		return BootstrapResult{}, fmt.Errorf("resolve suga home: %w", err)
	}
	result := BootstrapResult{
		Home:   home,
		Config: filepath.Join(home, "config.yaml"),
		Env:    filepath.Join(home, ".env"),
		Source: "home",
		Force:  opts.Force,
	}
	if explicit := strings.TrimSpace(os.Getenv("SUGA_CONFIG")); explicit != "" {
		result.Source = "explicit"
	}

	wasAbsent, err := pathAbsent(home)
	if err != nil {
		return result, fmt.Errorf("inspect suga home %s: %w", home, err)
	}
	if err := os.MkdirAll(home, 0o700); err != nil {
		return result, fmt.Errorf("create suga home %s: %w", home, err)
	}
	if err := secureBootstrapDir(home); err != nil {
		return result, fmt.Errorf("secure suga home %s: %w", home, err)
	}
	result.CreatedHome = wasAbsent

	lockDir := filepath.Join(home, "run", "locks")
	if err := os.MkdirAll(lockDir, 0o700); err != nil {
		return result, fmt.Errorf("create initialization lock directory %s: %w", lockDir, err)
	}
	if err := secureBootstrapDir(lockDir); err != nil {
		return result, fmt.Errorf("secure initialization lock directory %s: %w", lockDir, err)
	}
	lock, err := acquireBootstrapLock(filepath.Join(lockDir, "home.lock"))
	if err != nil {
		return result, fmt.Errorf("lock suga home initialization: %w", err)
	}
	defer func() { _ = lock.Close() }()

	// Re-check all paths after taking the cross-process lock. The first caller
	// may have created them while this process was waiting.
	configExists, err := pathExists(result.Config)
	if err != nil {
		return result, fmt.Errorf("inspect config %s: %w", result.Config, err)
	}
	envExists, err := pathExists(result.Env)
	if err != nil {
		return result, fmt.Errorf("inspect env %s: %w", result.Env, err)
	}

	if result.Source == "explicit" {
		if !envExists {
			if err := writeEmbeddedFile(result.Env, "onboarding.env", 0o600); err != nil {
				return result, fmt.Errorf("create env template %s: %w", result.Env, err)
			}
			result.CreatedEnv = true
		}
		explicit := strings.TrimSpace(os.Getenv("SUGA_CONFIG"))
		if _, err := os.Stat(explicit); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return result, fmt.Errorf("SUGA_CONFIG points to missing file %s; create it or unset SUGA_CONFIG and run suga init", explicit)
			}
			return result, fmt.Errorf("inspect SUGA_CONFIG %s: %w", explicit, err)
		}
		if opts.Force {
			return result, BootstrapError{Code: 2, Err: fmt.Errorf("SUGA_CONFIG is set; suga init --force will not overwrite %s", explicit)}
		}
		return result, nil
	}

	// Create .env before config so a later config failure can remove only the
	// file created by this invocation. An existing .env is never opened for write.
	if !envExists {
		if err := writeEmbeddedFile(result.Env, "onboarding.env", 0o600); err != nil {
			return result, fmt.Errorf("create env template %s: %w", result.Env, err)
		}
		result.CreatedEnv = true
	}
	if configExists && !opts.Force {
		return result, nil
	}

	if opts.Force && configExists {
		backup, err := createTimestampBackup(result.Config)
		if err != nil {
			if result.CreatedEnv {
				_ = os.Remove(result.Env)
			}
			return result, fmt.Errorf("backup config %s: %w", result.Config, err)
		}
		result.Backup = backup
	}
	if err := writeEmbeddedFile(result.Config, "onboarding.yaml", 0o600); err != nil {
		if result.CreatedEnv {
			_ = os.Remove(result.Env)
		}
		return result, fmt.Errorf("write config template %s: %w", result.Config, err)
	}
	result.CreatedConfig = true
	return result, nil
}

func pathAbsent(path string) (bool, error) {
	_, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	return false, err
}

// pathExists treats any existing directory, symlink, or file as user data. In
// particular, an existing .env is never replaced just because it is unusual.
func pathExists(path string) (bool, error) {
	_, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

func writeEmbeddedFile(path, name string, mode os.FileMode) error {
	data, err := onboarding.Files.ReadFile(name)
	if err != nil {
		return fmt.Errorf("read embedded %s: %w", name, err)
	}
	return atomicReplace(path, data, mode)
}

func atomicReplace(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	original, readErr := os.ReadFile(path)
	hadOriginal := readErr == nil
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return readErr
	}
	tmpName, err := writeBootstrapTemp(path, data, mode)
	if err != nil {
		return err
	}
	if err := replaceBootstrapFile(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := syncBootstrapDir(dir); err != nil {
		// A directory fsync failure happens after rename. Restore the exact
		// previous bytes (or remove a newly-created file) before reporting it.
		if restoreErr := restoreBootstrapFile(path, original, mode, hadOriginal); restoreErr != nil {
			return fmt.Errorf("sync directory after replacing %s: %w; restore: %v", path, err, restoreErr)
		}
		return fmt.Errorf("sync directory after replacing %s: %w", path, err)
	}
	return nil
}

func writeBootstrapTemp(path string, data []byte, mode os.FileMode) (string, error) {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		_ = tmp.Close()
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(mode); err != nil {
		return "", err
	}
	if _, err := tmp.Write(data); err != nil {
		return "", err
	}
	if err := tmp.Sync(); err != nil {
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	cleanup = false
	return tmpName, nil
}

func restoreBootstrapFile(path string, data []byte, mode os.FileMode, hadOriginal bool) error {
	if !hadOriginal {
		return os.Remove(path)
	}
	tmpName, err := writeBootstrapTemp(path, data, mode)
	if err != nil {
		return err
	}
	if err := replaceBootstrapFile(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

func createTimestampBackup(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	base := path + ".bak." + time.Now().UTC().Format("20060102T150405.000000000Z")
	for i := 0; ; i++ {
		candidate := base
		if i > 0 {
			candidate = fmt.Sprintf("%s.%d", base, i)
		}
		file, err := os.OpenFile(candidate, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		if err := writeSyncAndClose(file, data); err != nil {
			_ = os.Remove(candidate)
			return "", err
		}
		return candidate, nil
	}
}

func writeSyncAndClose(file *os.File, data []byte) error {
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

// WriteBootstrapNotice writes only the diagnostic initialization hint. The
// caller uses stderr for it so run stream-json and ACP protocol stdout remain
// machine-readable.
func WriteBootstrapNotice(w io.Writer, result BootstrapResult) error {
	if !result.CreatedConfig && !result.CreatedEnv {
		return nil
	}
	if result.Source == "explicit" {
		_, err := fmt.Fprintf(w, "已创建 suga .env：%s；当前配置来自 SUGA_CONFIG，未写入 home config\n", result.Env)
		return err
	}
	// Name only what this run created: a home that already had config.yaml and
	// only got its .env must not read as a fresh configuration.
	switch {
	case result.CreatedConfig && result.CreatedEnv:
		_, err := fmt.Fprintf(w, "已初始化 suga 配置：config=%s，env=%s；请编辑 config.yaml，并按 .env 注释填写变量\n", result.Config, result.Env)
		return err
	case result.CreatedConfig:
		_, err := fmt.Fprintf(w, "已创建 suga 配置：%s；请编辑 config.yaml\n", result.Config)
		return err
	default:
		_, err := fmt.Fprintf(w, "已创建 suga .env：%s；按其中注释填写变量\n", result.Env)
		return err
	}
}
