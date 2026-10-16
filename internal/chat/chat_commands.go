package chat

import (
	"fmt"
	"strings"
	"time"

	"github.com/inhere/suga/pkg/agentcore/tasks"
)

const (
	chatHelpText = `:session 显示会话
:compact 压缩上下文
:add-dir/:dirs/:rm-dir 管理附加目录
:diff [N] 显示文件改动
:help 显示此帮助
:usage 显示本会话用量与成本
:tasks 显示后台任务
:quit/:exit 退出
!<命令> 本地执行；!!<命令> 执行并附给下一条消息
按键：Enter/Alt+Enter/Ctrl+J 提交/换行；Esc 中止本轮；Ctrl+C 清空/中止/退出；回合中 Up 撤回插话；Up/Down 浏览历史`
	chatUsageHint  = ":usage 用量：本次 %d 轮 · 输入 %d / 输出 %d · 缓存读 %d / 写 %d · 成本 $%.6f；会话累计输入 %d / 输出 %d · 成本 $%.6f"
	chatTasksEmpty = "没有后台任务"
)

// BuildChatHelp returns the complete local-command and keyboard help text.
func BuildChatHelp() string { return chatHelpText }

// BuildChatUsage renders process and session usage using the same counters as the exit summary.
func BuildChatUsage(stats chatStats) string {
	return fmt.Sprintf(chatUsageHint, stats.turns, stats.usage.InputTokens, stats.usage.OutputTokens,
		stats.usage.CacheReadTokens, stats.usage.CacheWriteTokens, stats.usage.CostUSD,
		stats.session.InputTokens, stats.session.OutputTokens, stats.session.CostUSD)
}

// BuildChatTasks renders the task manager's stable start-order snapshot.
func BuildChatTasks(items []tasks.Info, now time.Time) string {
	if len(items) == 0 {
		return chatTasksEmpty
	}
	lines := make([]string, 0, len(items))
	for _, item := range items {
		last := item.Ended
		if last.IsZero() {
			last = item.Started
		}
		lines = append(lines, fmt.Sprintf("%s · %s · %s · 已运行 %s · 最近输出 %s · %s",
			item.ID, item.Status, item.Summary, formatElapsedAt(item.Duration(), now), last.Local().Format("15:04:05"), item.Kind))
	}
	return strings.Join(lines, "\n")
}

func formatElapsedAt(d time.Duration, _ time.Time) string { return formatElapsed(d) }

// runChatCommand handles commands typed at this terminal. The caller checks injected first.
func (s *chatState) runChatCommand(line string) bool {
	switch {
	case line == ":help":
		fmt.Fprintln(s.source, BuildChatHelp())
	case line == ":usage":
		fmt.Fprintln(s.source, BuildChatUsage(*s.stats))
	case line == ":tasks":
		if s.tasks == nil {
			fmt.Fprintln(s.source, chatTasksEmpty)
		} else {
			fmt.Fprintln(s.source, BuildChatTasks(s.tasks.List(s.sessionID), time.Now()))
		}
	default:
		return false
	}
	return true
}
