package lineedit

import (
	"bytes"
	"errors"
	"io"
	"os"
	"testing"

	"github.com/gookit/goutil/x/assert"
	"golang.org/x/term"
)

// TestMain follows the repo rule for new test packages: SUAG_HOME points at a
// throwaway directory and hooks are off, so tests never touch ~/.suag.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "suag-test-home-*")
	if err != nil {
		panic(err)
	}
	_ = os.Setenv("SUAG_HOME", home)
	_ = os.Setenv("SUAG_HOOKS", "off")
	code := m.Run()
	_ = os.RemoveAll(home)
	os.Exit(code)
}

// memTerm is an in-memory terminal: Read hands out queued input chunks and
// returns io.EOF when none are left; Write records everything.
type memTerm struct {
	in  []string
	out bytes.Buffer
}

func (m *memTerm) Read(p []byte) (int, error) {
	if len(m.in) == 0 {
		return 0, io.EOF
	}
	n := copy(p, m.in[0])
	m.in[0] = m.in[0][n:]
	if m.in[0] == "" {
		m.in = m.in[1:]
	}
	return n, nil
}

func (m *memTerm) Write(p []byte) (int, error) { return m.out.Write(p) }

// readAll feeds input and returns every line ReadLine completes before EOF.
func readAll(t *testing.T, tm *Terminal, mem *memTerm, input string) []string {
	t.Helper()
	mem.in = append(mem.in, input)
	var lines []string
	for {
		line, err := tm.ReadLine()
		if errors.Is(err, io.EOF) {
			return lines
		}
		assert.NoErr(t, err)
		lines = append(lines, line)
	}
}

func newMem(prompt string, width int) (*Terminal, *memTerm) {
	mem := &memTerm{}
	tm := NewTerminal(mem, prompt)
	if width > 0 {
		_ = tm.SetSize(width, 24)
	}
	return tm, mem
}

func TestRuneWidth(t *testing.T) {
	cases := map[rune]int{
		'a': 1, '你': 2, '，': 2, 'Ａ': 2, '😀': 2,
		'́': 0, '‍': 0, 'é': 1, 'а': 1, 'ب': 1, '\t': 1,
	}
	for r, want := range cases {
		assert.Eq(t, want, runeWidth(r), string(r))
	}
	assert.Eq(t, 7, visualLength([]rune("\x1b[32mhi,你好\x1b[0m")))
}

func TestTerminal_wideRunes(t *testing.T) {
	t.Run("backspace erases two columns per wide rune", func(t *testing.T) {
		tm, mem := newMem("", 0)
		lines := readAll(t, tm, mem, "你好a\x7f\x7f\r")
		assert.Eq(t, []string{"你"}, lines)
		// 'a': back 1, blank 1, back 1; '好': back 2, blank 2, back 2.
		assert.Eq(t, "你好a\x1b[D \x1b[D\x1b[2D  \x1b[2D\r\n", mem.out.String())
	})

	t.Run("left moves across a wide rune", func(t *testing.T) {
		tm, mem := newMem("", 0)
		lines := readAll(t, tm, mem, "a你b\x1b[D\x1b[Dc\r")
		assert.Eq(t, []string{"ac你b"}, lines)
		// Left over 'b' is 1 column, over '你' 2; after inserting 'c' the
		// cursor goes back 3 columns, and Enter moves right 3 to the end.
		assert.Eq(t, "a你b\x1b[D\x1b[2Dc你b\x1b[3D\x1b[3C\r\n", mem.out.String())
	})

	t.Run("right and ctrl-d step over a wide rune", func(t *testing.T) {
		tm, mem := newMem("", 0)
		lines := readAll(t, tm, mem, "你好\x01\x1b[C\x04\r")
		assert.Eq(t, []string{"你"}, lines)
		assert.Eq(t, "你好\x1b[4D\x1b[2C  \x1b[2D\r\n", mem.out.String())
	})

	t.Run("ctrl-u and ctrl-w blank by width", func(t *testing.T) {
		tm, mem := newMem("", 0)
		lines := readAll(t, tm, mem, "中文\x15x\r")
		assert.Eq(t, []string{"x"}, lines)
		assert.StrContains(t, mem.out.String(), "\x1b[4D    \x1b[4D")

		tm, mem = newMem("", 0)
		lines = readAll(t, tm, mem, "ab 中文\x17\r")
		assert.Eq(t, []string{"ab "}, lines)
		assert.StrContains(t, mem.out.String(), "\x1b[4D    \x1b[4D")
	})

	t.Run("combining mark goes with its base rune", func(t *testing.T) {
		tm, mem := newMem("", 0)
		lines := readAll(t, tm, mem, "xé\x7f\r")
		assert.Eq(t, []string{"x"}, lines)
	})

	t.Run("wraps at width 10", func(t *testing.T) {
		tm, mem := newMem("> ", 10)
		lines := readAll(t, tm, mem, "你好世界中\r")
		assert.Eq(t, []string{"你好世界中"}, lines)
		// 2 + 4*2 = 10 columns fill row 0 exactly, then '中' starts row 1.
		assert.Eq(t, "> 你好世界\r\n中\r\n> ", mem.out.String()) // ends with the next prompt
	})

	t.Run("wide rune never straddles the row end", func(t *testing.T) {
		tm, mem := newMem("> a", 10)
		lines := readAll(t, tm, mem, "你好世界\x7f\r")
		assert.Eq(t, []string{"你好世"}, lines)
		out := mem.out.String()
		// Only column 9 is left for '界': pad it with a space and wrap first.
		assert.StrContains(t, out, "> a你好世 \r\n界")
		// Backspace goes up to column 9 and blanks 3 cells: the pad + '界'.
		assert.StrContains(t, out, "界\x1b[A\x1b[7C   \x1b[A\x1b[7C")
		assert.Eq(t, 3, tm.cursorX) // after the next prompt "> a"
		assert.Eq(t, 0, tm.cursorY)
	})

	t.Run("left at a padded row start", func(t *testing.T) {
		tm, mem := newMem("> a", 10)
		lines := readAll(t, tm, mem, "你好世界\x1b[Dx\r")
		assert.Eq(t, []string{"你好世x界"}, lines)
		// 'x' fills column 9, so '界' now starts row 1 at column 0.
		assert.StrContains(t, mem.out.String(), "x\r\n界")
	})
}

// upstream mirrors the Terminal methods the parity test drives.
type upstream interface {
	ReadLine() (string, error)
	Write([]byte) (int, error)
	SetSize(int, int) error
	SetPrompt(string)
}

// TestTerminal_asciiParity drives x/term and lineedit with the same ASCII
// session and requires byte-identical output.
func TestTerminal_asciiParity(t *testing.T) {
	type step struct {
		input string // fed to ReadLine until EOF
		write string // passed to Write
		width int    // SetSize when > 0
	}
	sessions := map[string][]step{
		"edit keys": {{input: "hello world\x1b[D\x1b[D\x7fX\x01Y\x05Z\r"}},
		"words":     {{input: "one two  three\x17\x1b[1;3D\x1b[1;3Cq\x17\r"}},
		"kill":      {{input: "abcdef\x1b[D\x1b[D\x0b\x02\x02\x15gh\x04\r"}},
		"history":   {{input: "first\rsecond\r\x1b[A\x1b[A\x1b[B!\r"}},
		"wrap": {
			{width: 10},
			{input: "0123456789abcdefghijklmnop\x1b[D\x1b[D\x1b[D\x1b[D\x1b[D\x1b[D\x1b[D\x1b[D\x7f\x7f\x7fZ\r"},
		},
		"write redraw": {
			{width: 12},
			{input: "partial input text"},
			{write: "async output\n"},
			{input: "\x7f\r"},
		},
		"resize and clear": {
			{input: "some longer input line"},
			{width: 8},
			{width: 30},
			{input: "\x0c\r"},
		},
	}
	for name, steps := range sessions {
		t.Run(name, func(t *testing.T) {
			run := func(newTerm func(io.ReadWriter) upstream) (string, []string) {
				mem := &memTerm{}
				tm := newTerm(mem)
				tm.SetPrompt("$> ")
				var lines []string
				for _, s := range steps {
					switch {
					case s.width > 0:
						assert.NoErr(t, tm.SetSize(s.width, 24))
					case s.write != "":
						_, err := tm.Write([]byte(s.write))
						assert.NoErr(t, err)
					default:
						mem.in = append(mem.in, s.input)
						for {
							line, err := tm.ReadLine()
							if errors.Is(err, io.EOF) {
								break
							}
							assert.NoErr(t, err)
							lines = append(lines, line)
						}
					}
				}
				return mem.out.String(), lines
			}
			wantOut, wantLines := run(func(rw io.ReadWriter) upstream { return term.NewTerminal(rw, "") })
			gotOut, gotLines := run(func(rw io.ReadWriter) upstream { return NewTerminal(rw, "") })
			assert.Eq(t, wantLines, gotLines)
			assert.Eq(t, wantOut, gotOut)
		})
	}
}

func TestTerminal_pasteIndicator(t *testing.T) {
	tm, mem := newMem("", 0)
	mem.in = []string{"\x1b[200~你好\r\x1b[201~"}
	line, err := tm.ReadLine()
	assert.Eq(t, "你好", line)
	assert.True(t, errors.Is(err, ErrPasteIndicator))
}
