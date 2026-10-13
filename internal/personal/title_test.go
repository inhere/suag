package personal

import (
	"context"
	"strings"
	"testing"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suga/pkg/agentcore/mock"
)

// goferPrompt is shaped like a gofer job prompt: rules, tools and skills
// sections in front of the task.
const goferPrompt = `## 必须遵守的规则（gofer 注入，优先于本任务的其它说明）

### tracker-prime
- 用 gofer issue 跟踪任务
<!-- gofer:rules-end -->

## 本机可用工具（gofer 注入，按需使用）

本任务运行在 worker ` + "`w1`" + ` 上，以下工具已安装，可直接调用：

- ` + "`rg`" + ` — 搜索

## 可用技能（gofer 挂载，按需阅读）
- go-dev：Go 开发 → /tmp/skills/go-dev/SKILL.md
先读与本任务相关的 SKILL.md，再动手。

# 修复 sessions 列表的排序

细节……`

func TestSessionTitle(t *testing.T) {
	for _, tc := range []struct{ name, prompt, want string }{
		{"plain", "  fix the flaky test\nmore lines", "fix the flaky test"},
		{"gofer sections skipped", goferPrompt, "修复 sessions 列表的排序"},
		{"rules only", "## 必须遵守的规则（gofer 注入，优先于本任务的其它说明）\n- r\n<!-- gofer:rules-end -->\n\n跑一下测试", "跑一下测试"},
		{"a header without its end marker is kept", "## 必须遵守的规则（gofer 注入）\n- r", "必须遵守的规则（gofer 注入）"},
		{"empty", " \n ", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Eq(t, tc.want, SessionTitle(tc.prompt))
		})
	}
	t.Run("long first line clipped", func(t *testing.T) {
		got := SessionTitle(strings.Repeat("长", 150))
		assert.Eq(t, titleLimit, len([]rune(got)))
		assert.True(t, strings.HasSuffix(got, "…"))
	})
}

func TestRunTitlesSessions(t *testing.T) {
	ctx := context.Background()
	t.Run("a new session's title skips gofer's sections", func(t *testing.T) {
		runner, err := New(Config{Repository: openObserveRepo(t), Provider: mock.NewProvider(mock.Final("ok"))})
		assert.NoErr(t, err)
		res, err := runner.Run(ctx, RunRequest{Prompt: goferPrompt})
		assert.Require(t, assert.NoErr(t, err))
		s, err := runner.LoadSession(ctx, res.SessionID)
		assert.NoErr(t, err)
		assert.Eq(t, "修复 sessions 列表的排序", s.Title)
	})
	t.Run("an untitled session takes its first prompt", func(t *testing.T) {
		runner, err := New(Config{Repository: openObserveRepo(t), Provider: mock.NewProvider(mock.Final("ok"), mock.Final("ok"))})
		assert.NoErr(t, err)
		s, err := runner.NewSession(ctx, "", "") // ACP session/new
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, defaultTitle(s.ID), s.Title)
		_, err = runner.Run(ctx, RunRequest{SessionID: s.ID, Prompt: "first task"})
		assert.Require(t, assert.NoErr(t, err))
		_, err = runner.Run(ctx, RunRequest{SessionID: s.ID, Prompt: "second"})
		assert.Require(t, assert.NoErr(t, err))
		s, err = runner.LoadSession(ctx, s.ID)
		assert.NoErr(t, err)
		assert.Eq(t, "first task", s.Title)
	})
}
