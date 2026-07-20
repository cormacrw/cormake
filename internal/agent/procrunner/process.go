package procrunner

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"cormake/internal/agent"
)

// startRetryDelay/maxStartAttempts bound a narrow-race mitigation: if the
// resolved binary briefly doesn't exist at the exact moment of fork/exec
// (observed in practice — e.g. a self-update or security software
// transiently touching the file), one retry after a short delay absorbs it.
// A genuinely missing/broken install fails identically on the retry and
// still surfaces to the caller.
const (
	startRetryDelay  = 300 * time.Millisecond
	maxStartAttempts = 2
)

// tailDrainGrace is a short pause between the process ending (cmd.Wait
// returning, for Start; ProcessAlive going false, for Attach) and closing
// the tailers' stop channel — just enough to let a tailer's in-flight poll
// pick up the last bytes the process wrote before exiting.
const tailDrainGrace = 300 * time.Millisecond

// livenessPollInterval is how often Attach checks whether the process it's
// watching (which it did not itself start, and so cannot cmd.Wait on) is
// still alive.
const livenessPollInterval = time.Second

// Runner is the process-lifecycle half of an agent.Runner implementation:
// spawn (or reattach to) Binary, tail its raw stdout/stderr, and translate
// stdout lines via Translate into agent.Events. A backend package (see
// claude.Client/cursor.Client) embeds one of these configured with its own
// binary name and dialect parser, and just adds BuildArgs.
type Runner struct {
	// Binary is the CLI executable to exec (e.g. "claude", "cursor-agent").
	Binary string
	// Translate parses one raw stdout line in this backend's own
	// stream-json dialect into zero or more backend-neutral events.
	Translate func(taskID string, line []byte) []agent.Event
}

// Start spawns Binary with args, detached from cormake's controlling
// terminal, and returns a Handle streaming its translated output.
func (r Runner) Start(ctx context.Context, args []string, spec agent.RunSpec) (*agent.Handle, error) {
	var cmd *exec.Cmd
	var outFile, errFile *os.File
	var err error

	for attempt := 1; attempt <= maxStartAttempts; attempt++ {
		// A *exec.Cmd can't be reused after a failed Start, so build a
		// fresh one each attempt.
		cmd = exec.CommandContext(ctx, r.Binary, args...)
		cmd.Dir = spec.RepoPath
		// New session — not just a new process group — so the process
		// survives cormake's controlling terminal closing (e.g. cormake
		// quitting), not merely terminal-generated signals. A session
		// leader is always its own process group leader too, so the
		// existing Getpgid/Kill(-pgid, ...) signaling below needs no
		// change.
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

		outFile, err = os.OpenFile(spec.RawStdoutPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			return nil, err
		}
		errFile, err = os.OpenFile(spec.RawStderrPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			outFile.Close()
			return nil, err
		}
		// Real files, not pipes: a full buffer never blocks the child's
		// write(), regardless of whether cormake is currently reading (or
		// running at all) — this is what lets the process outlive cormake
		// pausing (tea.ExecProcess) or quitting entirely.
		cmd.Stdout = outFile
		cmd.Stderr = errFile

		err = cmd.Start()
		// The child gets its own dup'd fds on fork; our copies aren't
		// needed after Start returns either way.
		outFile.Close()
		errFile.Close()
		if err == nil {
			break
		}
		if attempt == maxStartAttempts || !errors.Is(err, fs.ErrNotExist) {
			return nil, r.diagnoseStartError(err, cmd)
		}
		time.Sleep(startRetryDelay)
	}

	pid := cmd.Process.Pid
	events := make(chan agent.Event, 64)
	stop := make(chan struct{})

	stdoutSink := func(line []byte) []agent.Event { return r.Translate(spec.TaskID, line) }
	stderrSink := StderrSink(spec.TaskID)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		TailFile(stop, spec.RawStdoutPath, 0, stdoutSink, func(ev agent.Event) { events <- ev })
	}()
	go func() {
		defer wg.Done()
		TailFile(stop, spec.RawStderrPath, 0, stderrSink, func(ev agent.Event) { events <- ev })
	}()
	go func() { wg.Wait(); close(events) }()

	waitErrCh := make(chan error, 1)
	go func() {
		waitErrCh <- cmd.Wait()
		time.Sleep(tailDrainGrace)
		close(stop)
	}()

	cancel, kill := signalHandle(pid)
	return &agent.Handle{
		Events: events,
		PID:    pid,
		Cancel: cancel,
		Kill:   kill,
		Wait:   func() error { return <-waitErrCh },
	}, nil
}

// Attach resumes tailing a process a previous Start call already launched
// (spec.PID) instead of spawning a new one. There is no real parent/child
// relationship here — cormake may not be, and after a restart never is,
// this process's actual OS parent — so completion is judged by polling
// ProcessAlive rather than cmd.Wait, and Wait always reports a nil error
// (see agent.Handle.Wait's doc comment for how callers should judge
// success/failure instead).
func (r Runner) Attach(ctx context.Context, spec agent.AttachSpec) (*agent.Handle, error) {
	events := make(chan agent.Event, 64)
	stop := make(chan struct{})

	stdoutSink := func(line []byte) []agent.Event { return r.Translate(spec.TaskID, line) }
	stderrSink := StderrSink(spec.TaskID)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		TailFile(stop, spec.RawStdoutPath, spec.Offset, stdoutSink, func(ev agent.Event) { events <- ev })
	}()
	go func() {
		defer wg.Done()
		// stderr isn't offset-tracked (see AttachSpec) — always re-tail
		// from the start; a handful of already-seen stderr lines
		// reappearing in the display log is cosmetic and low-stakes.
		TailFile(stop, spec.RawStderrPath, 0, stderrSink, func(ev agent.Event) { events <- ev })
	}()
	go func() { wg.Wait(); close(events) }()

	waitErrCh := make(chan error, 1)
	go func() {
		for ProcessAlive(spec.PID) {
			time.Sleep(livenessPollInterval)
		}
		waitErrCh <- nil
		time.Sleep(tailDrainGrace)
		close(stop)
	}()

	cancel, kill := signalHandle(spec.PID)
	return &agent.Handle{
		Events: events,
		PID:    spec.PID,
		Cancel: cancel,
		Kill:   kill,
		Wait:   func() error { return <-waitErrCh },
	}, nil
}

// signalHandle builds the Cancel/Kill closures shared by Start and Attach —
// both just signal pid's process group, whether or not this process is the
// one that actually started it.
func signalHandle(pid int) (cancel, kill func()) {
	cancel = func() {
		pgid, err := syscall.Getpgid(pid)
		if err != nil {
			return
		}
		syscall.Kill(-pgid, syscall.SIGTERM)
		time.AfterFunc(5*time.Second, func() {
			syscall.Kill(-pgid, syscall.SIGKILL)
		})
	}
	// kill is cancel's immediate counterpart: no grace period, because a
	// caller reaching for this (cormake quitting) may not be around long
	// enough for cancel's delayed SIGKILL to ever fire — confirmed directly:
	// a claude process that didn't die fast enough from SIGTERM survived as
	// an orphan once cormake's own process (and so its time.AfterFunc timer)
	// had already exited.
	kill = func() {
		pgid, err := syscall.Getpgid(pid)
		if err != nil {
			return
		}
		syscall.Kill(-pgid, syscall.SIGKILL)
	}
	return cancel, kill
}

// diagnoseStartError augments a failed cmd.Start() error with a fresh,
// independent LookPath(r.Binary) result plus the PATH/cwd this exact
// process actually sees — so the log line itself carries enough forensic
// detail to tell "briefly missing" apart from "resolves somewhere broken"
// apart from "PATH differs from what a login shell sees", without needing
// a separate round trip to ask for more diagnostics.
func (r Runner) diagnoseStartError(startErr error, cmd *exec.Cmd) error {
	lookPath, lookErr := exec.LookPath(r.Binary)
	wd, wdErr := os.Getwd()
	return fmt.Errorf(
		"%w [diagnostic: cmd.Path=%q cmd.Dir=%q fresh-LookPath=%q fresh-LookPath-err=%v process-cwd=%q process-cwd-err=%v PATH=%q]",
		startErr, cmd.Path, cmd.Dir, lookPath, lookErr, wd, wdErr, os.Getenv("PATH"),
	)
}
