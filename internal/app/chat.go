package app

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
)

func (a *App) RunChat(ctx context.Context) {
	fmt.Println("axon chat mode — type your task and press Enter. Ctrl+C to exit.")
	fmt.Println("Commands: :quit, :hitl (list pending tickets), :respond <id> <instruction>")
	fmt.Println()

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			break
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		switch {
		case line == ":quit":
			return
		case line == ":hitl":
			tickets := a.hitlMgr.ListPending()
			if len(tickets) == 0 {
				fmt.Println("No pending HITL tickets.")
			}
			for _, t := range tickets {
				fmt.Printf("  [%s] agent=%s reason=%s\n", t.ID, t.AgentID, t.Reason)
			}
		case strings.HasPrefix(line, ":respond "):
			parts := strings.SplitN(line[9:], " ", 2)
			if len(parts) < 2 {
				fmt.Println("Usage: :respond <ticket-id> <instruction>")
				continue
			}
			if err := a.hitlMgr.Respond(parts[0], parts[1]); err != nil {
				fmt.Println("Error:", err)
			} else {
				fmt.Println("Instruction delivered.")
			}
		default:
			fmt.Println("Running task...")
			result, err := a.orch.RunTask(ctx, line)
			if err != nil {
				fmt.Println("Error:", err)
				continue
			}
			fmt.Println("\n─── Result ───────────────────────────────────────")
			fmt.Println(result.Output)
			fmt.Println("──────────────────────────────────────────────────")
		}
	}
}
