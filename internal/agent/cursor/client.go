// Package cursor implements agent.Runner by shelling out to the
// cursor-agent CLI and translating its stream-json output into agent.Event
// values. The process-lifecycle plumbing (spawn/tail/signal/attach) has
// nothing cursor-specific about it and lives in internal/agent/procrunner,
// shared with internal/agent/claude.
package cursor

import (
	"context"

	"cormake/internal/agent"
	"cormake/internal/agent/procrunner"
)

type Client struct{}

var _ agent.Runner = Client{}

var runner = procrunner.Runner{Binary: "cursor-agent", Translate: translateLine}

func (Client) Start(ctx context.Context, spec agent.RunSpec) (*agent.Handle, error) {
	return runner.Start(ctx, BuildArgs(spec), spec)
}

func (Client) Attach(ctx context.Context, spec agent.AttachSpec) (*agent.Handle, error) {
	return runner.Attach(ctx, spec)
}

// ReplayFile does a bounded "read whatever's on disk right now" pass over
// path, parsed through cursor-agent's own stream-json dialect — see
// procrunner.ReplayFile.
func ReplayFile(taskID, path string, fromOffset int64) (events []agent.Event, finalOffset int64) {
	return procrunner.ReplayFile(taskID, path, fromOffset, translateLine)
}
