// agentpoc is a throwaway harness for validating internal/agent/claude
// against a real `claude` process — not part of the app itself.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/google/uuid"

	"cormake/internal/agent"
	"cormake/internal/agent/claude"
)

func main() {
	repoPath := flag.String("repo", ".", "repo path to run in")
	prompt := flag.String("prompt", "list the top-level files in this repo and summarize what this project is in two sentences", "prompt to send")
	flag.Parse()

	spec := agent.RunSpec{
		TaskID:    "poc-task",
		SessionID: uuid.NewString(),
		Prompt:    *prompt,
		RepoPath:  *repoPath,
		Mode:      agent.RunModePlan,
	}

	fmt.Println("spawning: claude", claude.BuildArgs(spec))
	fmt.Println("---")

	c := claude.Client{}
	handle, err := c.Start(context.Background(), spec)
	if err != nil {
		fmt.Fprintln(os.Stderr, "start error:", err)
		os.Exit(1)
	}

	for ev := range handle.Events {
		switch ev.Type {
		case agent.EventInit:
			fmt.Printf("[init] session=%s model=%s\n", ev.SessionID, ev.Text)
		case agent.EventText:
			prefix := "assistant"
			if ev.IsSubagent {
				prefix = "subagent"
			}
			fmt.Printf("[%s] %s\n", prefix, ev.Text)
		case agent.EventToolUse:
			fmt.Printf("[tool_use] %s(%s)\n", ev.ToolName, ev.ToolInput)
		case agent.EventToolResult:
			fmt.Printf("[tool_result] %s\n", truncate(ev.Text, 200))
		case agent.EventResult:
			fmt.Printf("[result] cost=$%.4f session=%s\n%s\n", ev.CostUSD, ev.SessionID, ev.ResultText)
		case agent.EventStderrLine:
			fmt.Printf("[stderr] %s\n", ev.Text)
		case agent.EventProcessError:
			fmt.Printf("[process_error] %v\n", ev.Err)
		}
	}

	if err := handle.Wait(); err != nil {
		fmt.Fprintln(os.Stderr, "process exited with error:", err)
		os.Exit(1)
	}
	fmt.Println("---")
	fmt.Println("done")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
