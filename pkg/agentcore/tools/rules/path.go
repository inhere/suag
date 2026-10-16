package rules

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

// MatchesPath applies a file rule to a resolved tool target. Allow rules are
// deliberately stricter than deny rules: the lexical path and the path after
// resolving its existing ancestors must both match, and the target must not be
// a link, hard link, or special file. A deny rule is conservative in the other
// direction and matches either spelling, including a failed resolution.
func (r Rule) MatchesPath(target Target) bool {
	if len(r.Path) == 0 || target.MCP || !isFileTool(r.Tool) || target.Tool != r.Tool {
		return false
	}
	if !rootsMatch(r.Root, target.Root) {
		return false
	}
	resolved, ok := resolveTargetPath(target.Root, target.Path)
	if !ok {
		return false
	}
	if r.Action == Deny && !resolved.realOK {
		return true
	}
	for _, pattern := range r.Path {
		lexical := pathPatternMatches(pattern, resolved.relative, resolved.absolute)
		real := resolved.realOK && pathPatternMatches(pattern, resolved.realRelative, resolved.real)
		if r.Action == Allow {
			if lexical && real && resolved.allowSafe {
				return true
			}
			continue
		}
		if lexical || real {
			return true
		}
	}
	return false
}

type resolvedPath struct {
	root, absolute, relative     string
	realRoot, real, realRelative string
	realOK, allowSafe            bool
}

func rootsMatch(ruleRoot, targetRoot string) bool {
	ruleRoot, ruleErr := filepath.Abs(filepath.Clean(ruleRoot))
	targetRoot, targetErr := filepath.Abs(filepath.Clean(targetRoot))
	if ruleErr != nil || targetErr != nil || ruleRoot == "" || targetRoot == "" {
		return false
	}
	ruleReal, ruleRealErr := filepath.EvalSymlinks(ruleRoot)
	targetReal, targetRealErr := filepath.EvalSymlinks(targetRoot)
	if ruleRealErr == nil && targetRealErr == nil {
		return samePath(ruleReal, targetReal)
	}
	return samePath(ruleRoot, targetRoot)
}

// resolveTargetPath keeps path matching tied to the same root that the native
// file tools use. The deepest existing ancestor is resolved so a rule can
// match a file that a file_write call is about to create.
func resolveTargetPath(root, target string) (resolvedPath, bool) {
	root, err := filepath.Abs(filepath.Clean(root))
	if err != nil || root == "" || target == "" {
		return resolvedPath{}, false
	}
	if !filepath.IsAbs(target) && filepath.VolumeName(target) != "" {
		return resolvedPath{}, false
	}
	absolute := filepath.Clean(target)
	if !filepath.IsAbs(absolute) {
		absolute = filepath.Join(root, absolute)
	}
	relative, inside := relativePath(root, absolute)
	if !inside {
		return resolvedPath{}, false
	}
	base := resolvedPath{
		root:     root,
		absolute: absolute,
		relative: filepath.ToSlash(relative),
	}

	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return base, true
	}
	realRoot, err = filepath.Abs(filepath.Clean(realRoot))
	if err != nil {
		return base, true
	}
	existing, tail, info, err := deepestExisting(absolute)
	if err != nil || info == nil {
		return base, true
	}
	realExisting, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return base, true
	}
	realExisting, err = filepath.Abs(filepath.Clean(realExisting))
	if err != nil {
		return base, true
	}
	real := filepath.Clean(filepath.Join(append([]string{realExisting}, tail...)...))
	realRelative, realInside := relativePath(realRoot, real)
	if !realInside {
		return base, true
	}

	// Comparing the resolved ancestor with the expected real root spelling
	// detects links below the approved root without rejecting a symlinked root.
	existingRelative, existingInside := relativePath(root, existing)
	expectedExisting := filepath.Join(realRoot, existingRelative)
	internalLink := !existingInside || !samePath(realExisting, expectedExisting)
	finalUnsafe := len(tail) == 0 && (!info.Mode().IsRegular() || hardLinked(existing, info))
	if len(tail) > 0 && !info.IsDir() {
		return base, true
	}

	base.realRoot = realRoot
	base.real = real
	base.realRelative = filepath.ToSlash(realRelative)
	base.realOK = true
	base.allowSafe = !internalLink && !finalUnsafe && !windowsUnsafePath(absolute)
	return base, true
}

// deepestExisting returns the longest existing prefix and the path components
// that are not present yet. Only ENOENT is treated as an expected missing tail;
// permission, malformed-name, and other lookup errors fail closed.
func deepestExisting(target string) (string, []string, os.FileInfo, error) {
	current := target
	var tail []string
	for {
		info, err := os.Lstat(current)
		if err == nil {
			return current, tail, info, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", nil, nil, err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", nil, nil, err
		}
		tail = append([]string{filepath.Base(current)}, tail...)
		current = parent
	}
}

func relativePath(root, target string) (string, bool) {
	rel, err := filepath.Rel(root, target)
	if err != nil || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return rel, true
}

func samePath(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

func pathPatternMatches(pattern, relative, absolute string) bool {
	pattern = filepath.ToSlash(pattern)
	candidate := relative
	if filepath.IsAbs(pattern) {
		candidate = filepath.ToSlash(absolute)
	}
	return globPath(pattern, candidate)
}

// globPath matches slash-separated path segments. Unlike path.Match, ** is a
// recursive directory wildcard while * remains exactly one path segment.
func globPath(pattern, candidate string) bool {
	patternAbsolute, patternParts := splitGlobPath(pattern)
	candidateAbsolute, candidateParts := splitGlobPath(candidate)
	if patternAbsolute != candidateAbsolute {
		return false
	}
	return globSegments(patternParts, candidateParts)
}

func splitGlobPath(value string) (bool, []string) {
	value = strings.TrimPrefix(filepath.ToSlash(value), "./")
	absolute := strings.HasPrefix(value, "/")
	value = strings.TrimLeft(value, "/")
	if value == "" {
		return absolute, nil
	}
	return absolute, strings.Split(value, "/")
}

func globSegments(pattern, candidate []string) bool {
	if len(pattern) == 0 {
		return len(candidate) == 0
	}
	if pattern[0] == "**" {
		return globSegments(pattern[1:], candidate) || len(candidate) > 0 && globSegments(pattern, candidate[1:])
	}
	if len(candidate) == 0 {
		return false
	}
	p, c := pattern[0], candidate[0]
	if runtime.GOOS == "windows" {
		p, c = strings.ToLower(p), strings.ToLower(c)
	}
	matched, err := path.Match(p, c)
	return err == nil && matched && globSegments(pattern[1:], candidate[1:])
}

func windowsUnsafePath(value string) bool {
	if runtime.GOOS != "windows" {
		return false
	}
	volume := filepath.VolumeName(value)
	rest := strings.TrimPrefix(value, volume)
	for _, part := range strings.FieldsFunc(rest, func(r rune) bool { return r == '\\' || r == '/' }) {
		if strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") || shortNamePart(part) {
			return true
		}
	}
	// A colon after the volume name denotes an alternate data stream.
	return strings.Contains(rest, ":")
}

func shortNamePart(part string) bool {
	for i := 0; i+1 < len(part); i++ {
		if part[i] == '~' && part[i+1] >= '0' && part[i+1] <= '9' {
			return true
		}
	}
	return false
}
