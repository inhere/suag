package chat

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

var chatCommands = []string{":session", ":compact", ":add-dir", ":dirs", ":rm-dir", ":diff", ":quit", ":exit"}

// installCompletion wires the line editor's callback to local chat commands and
// directory arguments. Completion only acts at the command line and never on a
// delivered message.
func installCompletion(source *chatInputSource, dirs SessionDirs) {
	if source.terminal == nil {
		return
	}
	source.terminal.AutoCompleteCallback = func(line string, pos int, key rune) (string, int, bool) {
		if key != '\t' || pos != len(line) || !strings.HasPrefix(line, ":") {
			return "", 0, false
		}
		name, arg := splitCommand(line)
		if !strings.ContainsAny(line, " \t") {
			var matches []string
			for _, command := range chatCommands {
				if strings.HasPrefix(command, line) {
					matches = append(matches, command)
				}
			}
			return completeCandidates(source, line, matches)
		}
		if dirs == nil || (name != addDirCmd && name != rmDirCmd) {
			return "", 0, false
		}
		return completePath(source, line, name, arg)
	}
}

func completeCandidates(source *chatInputSource, line string, matches []string) (string, int, bool) {
	if len(matches) == 0 {
		return "", 0, false
	}
	if len(matches) > 1 {
		_, _ = fmt.Fprintln(source, strings.Join(matches, "  "))
		return line, len(line), true
	}
	return matches[0], len(matches[0]), true
}

func completePath(source *chatInputSource, line, name, arg string) (string, int, bool) {
	base := arg
	prefix := ""
	if i := strings.LastIndexAny(arg, `/\\`); i >= 0 {
		prefix, base = arg[:i+1], arg[i+1:]
	}
	dir := prefix
	if dir == "" {
		dir = "."
	}
	items, err := os.ReadDir(dir)
	if err != nil {
		return "", 0, false
	}
	var matches []string
	for _, item := range items {
		if item.IsDir() && strings.HasPrefix(item.Name(), base) {
			matches = append(matches, item.Name())
		}
	}
	sort.Strings(matches)
	if len(matches) == 0 {
		return "", 0, false
	}
	if len(matches) > 1 {
		_, _ = fmt.Fprintln(source, strings.Join(matches, "  "))
		return line, len(line), true
	}
	completed := name + " " + prefix + matches[0]
	return completed, len(completed), true
}
