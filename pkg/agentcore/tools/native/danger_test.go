package native

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/gookit/goutil/x/assert"
)

func TestDangerousDelete(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	ws := filepath.Join(home, "work", "proj")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	check := func(t *testing.T, powershell bool, command, want string) {
		t.Helper()
		got := DangerousDelete(command, powershell, ws)
		if want == "" {
			assert.Eq(t, "", got, command)
			return
		}
		assert.StrContains(t, got, want, command)
	}

	t.Run("sh: dangerous", func(t *testing.T) {
		for command, want := range map[string]string{
			`rm -rf /`:                         "删除根目录",
			`rm -rf /*`:                        "清空根目录",
			`sudo rm -rf --no-preserve-root /`: "根目录",
			`rm -rf ~`:                         "删除用户主目录",
			`rm -rf ~/`:                        "用户主目录",
			`rm ~/*`:                           "清空用户主目录", // a glob counts without -r
			`rm -rf "$HOME"`:                   "用户主目录",
			`rm -fr ${HOME}/`:                  "用户主目录",
			`rm -r -- ~`:                       "用户主目录",
			`rm -rf "$BUILD_DIR/"`:             "变量 $BUILD_DIR 开头",
			`rm -rf $X/*`:                      "变量 $X 开头",
			`rm -R "$(pwd)/"`:                  "变量 $(pwd) 开头",
			`rm -rf .`:                         "删除整个工作区",
			`rm -rf *`:                         "清空整个工作区",
			`rm -rf ..`:                        "工作区的上级目录",
			`rm -rf ~/work`:                    "工作区的上级目录",
			`cd .. && rm -rf *`:                "清空工作区的上级目录",
			`cd ~; rm -rf *`:                   "清空用户主目录",
			`make clean; rm -rf ~`:             "用户主目录",
			`bash -c "rm -rf ~"`:               "用户主目录",
			`sh -lc 'rm -r $HOME'`:             "用户主目录",
			`env X=1 /bin/rm -rf ~`:            "用户主目录",
			`rm -rf ` + filepath.ToSlash(home): "用户主目录", // sh reads \ as an escape
			`rm -rf ` + filepath.ToSlash(ws):   "删除整个工作区",
		} {
			check(t, false, command, want)
		}
	})
	t.Run("sh: not dangerous", func(t *testing.T) {
		for _, command := range []string{
			`rm -rf build dist node_modules`,
			`rm -f ~/.cache/x.lock`,
			`rm -rf ~/.cache/go-build`,
			`rm -rf "$HOME/.cache/x"`,
			`rm -rf /tmp/x`,
			`rm -rf "$X"`, // an empty $X is no path at all
			`rm ~`,        // not recursive, not a glob
			`cd build && rm -rf *`,
			`echo rm -rf ~`,
			`git rm -r src/old`,
			`grep -rn "rm -rf /" .`,
			`# rm -rf ~`,
			`cd "$X" && rm -rf *`, // unknown directory: cannot tell
		} {
			check(t, false, command, "")
		}
	})
	t.Run("PowerShell and cmd: dangerous", func(t *testing.T) {
		for command, want := range map[string]string{
			`Remove-Item -Recurse -Force ~`:                       "用户主目录",
			`Remove-Item $env:USERPROFILE -Recurse`:               "用户主目录",
			`Remove-Item -LiteralPath $env:USERPROFILE\ -Recurse`: "用户主目录",
			`rm -r -fo C:\`:                         "盘符根目录 C:",
			`Remove-Item "$dir\*" -Recurse -Force`:  "变量 $dir 开头",
			`Remove-Item -Path "$target\" -Recurse`: "变量 $target 开头",
			`ri -r ..`:                              "工作区的上级目录",
			`Get-ChildItem | ForEach-Object { Remove-Item $HOME -Recurse }`: "用户主目录",
			`cmd /c "rd /s /q C:\"`:                                          "盘符根目录",
			`cmd /c rd /s /q %USERPROFILE%`:                                  "用户主目录",
			`pwsh -NoProfile -Command "Remove-Item -Recurse -Force $HOME\*"`: "清空用户主目录",
		} {
			check(t, true, command, want)
		}
	})
	t.Run("PowerShell and cmd: not dangerous", func(t *testing.T) {
		for _, command := range []string{
			`Remove-Item -Recurse -Force build`,
			`Remove-Item $env:TEMP\x -Recurse`,
			`Remove-Item "$dir"`,
			`Remove-Item -Path .\out -Recurse`,
			`Remove-Item foo.txt`,
			`Remove-Item -Recurse -Filter *.log -Path logs`,
			`cmd /c del /q out\*.obj`,
			`cmd /c rd /s /q build`,
			`Write-Output "Remove-Item -Recurse ~"`,
		} {
			check(t, true, command, "")
		}
	})
	t.Run("the reason names the command", func(t *testing.T) {
		got := DangerousDelete(`ls; rm -rf ~`, false, ws)
		assert.True(t, strings.HasPrefix(got, "rm -rf ~："), got)
	})
}
