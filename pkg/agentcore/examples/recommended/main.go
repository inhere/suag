// Command recommended assembles a read-only agent from the agentcore layers:
// an llm provider, the tools registry with the read-only native tools,
// workspace skills, an optional MCP server and an approval policy.
//
// Without OPENAI_API_KEY it runs offline with a scripted mock provider. Set
// OPENAI_API_KEY (and optionally OPENAI_BASE_URL, OPENAI_MODEL) to use a real
// model, and AGENTCORE_MCP_COMMAND to attach one stdio MCP server.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/inhere/suga/pkg/agentcore"
	"github.com/inhere/suga/pkg/agentcore/llm"
	"github.com/inhere/suga/pkg/agentcore/mock"
	"github.com/inhere/suga/pkg/agentcore/skills"
	"github.com/inhere/suga/pkg/agentcore/tools"
	"github.com/inhere/suga/pkg/agentcore/tools/mcp"
	"github.com/inhere/suga/pkg/agentcore/tools/native"
)

// staticPrompt is the simplest PromptBuilder: one fixed system prompt.
type staticPrompt string

func (p staticPrompt) BuildSystemPrompt(context.Context, *agentcore.RunState) (string, error) {
	return string(p), nil
}

func main() {
	ctx := context.Background()
	workspace, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}

	// Tools: only the read-only native tools (file_read, file_list, file_grep,
	// file_find, web_fetch; web_search needs a SerpAPI key).
	reg := tools.NewRegistry()
	for _, tool := range native.BuildTools(native.Env{Workspace: workspace}, false) {
		reg.Register(tool)
	}

	// Skills: SKILL.md files under <workspace>/.agents/skills, read on demand
	// through the skill tool.
	skillReg := skills.LoadRoots([]skills.Root{{Dir: filepath.Join(workspace, ".agents", "skills"), Workspace: true}})
	if skillReg.Len() > 0 {
		reg.Register(native.NewSkillTool(skillReg, 0))
	}

	// Optional MCP server; its tools are MCP kind and never count as read-only.
	if command := os.Getenv("AGENTCORE_MCP_COMMAND"); command != "" {
		fields := strings.Fields(command)
		server, err := mcp.NewProvider(ctx, mcp.ServerSettings{ID: "example", Command: fields[0], Args: fields[1:], CallTimeout: time.Minute})
		if err != nil {
			log.Fatal(err)
		}
		defer server.Close()
		if err := server.RegisterTools(reg); err != nil {
			log.Fatal(err)
		}
	}

	// Approval: read-only tools run, everything else is refused. A real host
	// asks the user here (see tools.Asker) instead of refusing.
	approval := tools.NewApproval(reg, tools.ApproverFunc(func(_ context.Context, call agentcore.ToolCall, tool agentcore.Tool) error {
		if tools.MetaOf(tool).ReadOnly {
			return nil
		}
		return tools.Denied(fmt.Sprintf("%s needs approval, which this example does not grant", call.Name))
	}))

	// Storage is left unset: runs live in memory for this process only. Pass
	// files.NewStorage(dir) to resume runs across processes.
	runtime := agentcore.New(agentcore.Config{
		Provider:      provider(),
		Tools:         approval,
		PromptBuilder: staticPrompt("You are a read-only assistant for the current workspace. Use tools to look before answering."),
		MaxSteps:      8,
	})

	instruction := "List the top-level files of the workspace and say what the project is."
	if len(os.Args) > 1 {
		instruction = strings.Join(os.Args[1:], " ")
	}
	result, err := runtime.Run(ctx, agentcore.RunInput{AgentID: "recommended", Instruction: instruction})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("status=%s steps=%d\n%s\n", result.Status, result.Steps, result.Output)
}

// provider returns the OpenAI-compatible llm provider when OPENAI_API_KEY is
// set, otherwise a scripted mock that lists the workspace and answers.
func provider() agentcore.Provider {
	if key := os.Getenv("OPENAI_API_KEY"); key != "" {
		return llm.NewOpenAIProvider(llm.OpenAIConfig{APIKey: key, BaseURL: os.Getenv("OPENAI_BASE_URL"), Model: os.Getenv("OPENAI_MODEL")})
	}
	return mock.NewProvider(
		mock.ToolCall("call-list", "file_list", map[string]any{"path": "."}),
		mock.Final("(mock) listed the workspace; set OPENAI_API_KEY for a real answer"),
	)
}
