// Package claude implements agent.Runner by shelling out to the claude CLI
// and translating its stream-json output into agent.Event values. The
// process-lifecycle plumbing (spawn/tail/signal/attach) has nothing
// claude-specific about it and lives in internal/agent/procrunner, shared
// with internal/agent/cursor.
package claude

import (
	"context"

	"cormake/internal/agent"
	"cormake/internal/agent/procrunner"
)

type Client struct{}

var _ agent.Runner = Client{}

var runner = procrunner.Runner{Binary: "claude", Translate: translateLine}

func (Client) Start(ctx context.Context, spec agent.RunSpec) (*agent.Handle, error) {
	return runner.Start(ctx, BuildArgs(spec), spec)
}

func (Client) Attach(ctx context.Context, spec agent.AttachSpec) (*agent.Handle, error) {
	return runner.Attach(ctx, spec)
}

// ProcessAlive reports whether pid refers to a live process — see
// procrunner.ProcessAlive. Kept re-exported here since callers outside this
// package (see ui.reconnectTask) already reference claude.ProcessAlive as a
// backend-neutral file/PID check.
func ProcessAlive(pid int) bool {
	return procrunner.ProcessAlive(pid)
}

// ReplayFile does a bounded "read whatever's on disk right now" pass over
// path, parsed through claude's own stream-json dialect — see
// procrunner.ReplayFile.
func ReplayFile(taskID, path string, fromOffset int64) (events []agent.Event, finalOffset int64) {
	return procrunner.ReplayFile(taskID, path, fromOffset, translateLine)
}
