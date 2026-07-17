// Package claude implements agent.Runner by shelling out to the claude CLI
// and translating its stream-json output into agent.Event values.
package claude

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"cormake/internal/agent"
)

type Client struct{}

var _ agent.Runner = Client{}

// startRetryDelay/maxStartAttempts bound a narrow-race mitigation: if the
// resolved claude binary briefly doesn't exist at the exact moment of
// fork/exec (observed in practice — e.g. a self-update or security
// software transiently touching the file), one retry after a short delay
// absorbs it. A genuinely missing/broken install fails identically on the
// retry and still surfaces to the caller.
const (
	startRetryDelay  = 300 * time.Millisecond
	maxStartAttempts = 2
)

func (Client) Start(ctx context.Context, spec agent.RunSpec) (*agent.Handle, error) {
	var cmd *exec.Cmd
	var stdout, stderr io.ReadCloser
	var err error

	for attempt := 1; attempt <= maxStartAttempts; attempt++ {
		// A *exec.Cmd can't be reused after a failed Start, so build a
		// fresh one each attempt.
		cmd = exec.CommandContext(ctx, "claude", BuildArgs(spec)...)
		cmd.Dir = spec.RepoPath
		// Own process group so Cancel can kill the whole tree (claude may
		// spawn its own children), not just the immediate process.
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

		stdout, err = cmd.StdoutPipe()
		if err != nil {
			return nil, err
		}
		stderr, err = cmd.StderrPipe()
		if err != nil {
			return nil, err
		}

		err = cmd.Start()
		if err == nil {
			break
		}
		if attempt == maxStartAttempts || !errors.Is(err, fs.ErrNotExist) {
			return nil, diagnoseStartError(err, cmd)
		}
		time.Sleep(startRetryDelay)
	}

	events := make(chan agent.Event, 64)

	// Both readers write to the same channel; only close it once both have
	// hit EOF (process exit), or a send-after-close panic is possible if
	// stderr is still producing lines after stdout's reader finishes first.
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); readStdout(spec.TaskID, stdout, events) }()
	go func() { defer wg.Done(); readStderr(spec.TaskID, stderr, events) }()
	go func() { wg.Wait(); close(events) }()

	cancel := func() {
		if cmd.Process == nil {
			return
		}
		pgid, err := syscall.Getpgid(cmd.Process.Pid)
		if err != nil {
			return
		}
		syscall.Kill(-pgid, syscall.SIGTERM)
		time.AfterFunc(5*time.Second, func() {
			syscall.Kill(-pgid, syscall.SIGKILL)
		})
	}

	// cmd.Wait must not be called until both pipes are fully drained (see
	// os/exec docs), which readStdout/readStderr guarantee by running to
	// EOF before wg.Wait() unblocks — so it's safe for the caller to call
	// Wait() any time after it's done ranging over Events.
	wait := cmd.Wait

	return &agent.Handle{Events: events, Cancel: cancel, Wait: wait}, nil
}

// diagnoseStartError augments a failed cmd.Start() error with a fresh,
// independent LookPath("claude") result plus the PATH/cwd this exact
// process actually sees — so the log line itself carries enough forensic
// detail to tell "briefly missing" apart from "resolves somewhere broken"
// apart from "PATH differs from what a login shell sees", without needing
// a separate round trip to ask for more diagnostics.
func diagnoseStartError(startErr error, cmd *exec.Cmd) error {
	lookPath, lookErr := exec.LookPath("claude")
	wd, wdErr := os.Getwd()
	return fmt.Errorf(
		"%w [diagnostic: cmd.Path=%q cmd.Dir=%q fresh-LookPath=%q fresh-LookPath-err=%v process-cwd=%q process-cwd-err=%v PATH=%q]",
		startErr, cmd.Path, cmd.Dir, lookPath, lookErr, wd, wdErr, os.Getenv("PATH"),
	)
}

func readStdout(taskID string, r io.Reader, events chan<- agent.Event) {
	scanner := bufio.NewScanner(r)
	// tool_result lines can be large; grow well past the default 64KB cap.
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	for scanner.Scan() {
		for _, ev := range translateLine(taskID, scanner.Bytes()) {
			events <- ev
		}
	}
}

func readStderr(taskID string, r io.Reader, events chan<- agent.Event) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		events <- agent.Event{TaskID: taskID, Type: agent.EventStderrLine, Text: scanner.Text()}
	}
}
