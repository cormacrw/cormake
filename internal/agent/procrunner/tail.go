// Package procrunner holds the process-lifecycle and file-tailing plumbing
// shared by every agent.Runner implementation (see internal/agent/claude,
// internal/agent/cursor) — spawning a detached CLI process, tailing its raw
// stdout/stderr into translated agent.Events, and signaling/attaching to it
// later. None of it is specific to any one backend's CLI or wire dialect;
// each backend supplies only its binary name and a LineSink that knows how
// to parse its own stream-json flavor.
package procrunner

import (
	"bytes"
	"os"
	"syscall"
	"time"

	"cormake/internal/agent"
)

// tailPollInterval is how often TailFile retries after catching up to the
// writer with no complete line yet available — short enough that live
// streaming still feels immediate, long enough not to busy-loop.
const tailPollInterval = 200 * time.Millisecond

// LineSink turns one raw output line into zero or more translated events.
type LineSink func(line []byte) []agent.Event

// StderrSink turns a stderr line into a single EventStderrLine — identical
// across every backend, since stderr has no structured dialect to parse.
func StderrSink(taskID string) LineSink {
	return func(line []byte) []agent.Event {
		return []agent.Event{{TaskID: taskID, Type: agent.EventStderrLine, Text: string(line)}}
	}
}

// TailFile reads path from startOffset forward, splitting complete lines and
// running each through sink, stamping the resulting events' Offset and
// passing them to emit. pending simply grows until a newline appears, so
// there's no scanner buffer-size cap to worry about for a long tool_result
// line.
//
// If stop isn't yet closed when a read catches up to the current end of the
// file with no complete line pending, TailFile sleeps tailPollInterval and
// retries — this is what makes it usable as a live "tail -f". Once stop is
// closed, it does exactly one more read-to-EOF pass (to catch anything
// written in the gap between the writer dying and the caller noticing) and
// returns. Passing an already-closed stop turns this into a bounded
// "replay whatever is on disk right now" scan (see ReplayFile).
func TailFile(stop <-chan struct{}, path string, startOffset int64, sink LineSink, emit func(agent.Event)) (finalOffset int64) {
	f, err := os.Open(path)
	if err != nil {
		// Nothing to tail (e.g. deleted out from under us, or a reconnect
		// racing a run that hasn't written anything yet) — degrade to "no
		// new content" rather than erroring the caller.
		return startOffset
	}
	defer f.Close()

	offset := startOffset
	var pending []byte
	buf := make([]byte, 64*1024)

	drain := func() {
		for {
			n, err := f.ReadAt(buf, offset)
			if n > 0 {
				pending = append(pending, buf[:n]...)
				offset += int64(n)
				for {
					i := bytes.IndexByte(pending, '\n')
					if i < 0 {
						break
					}
					line := pending[:i]
					pending = pending[i+1:]
					lineEndOffset := offset - int64(len(pending))
					for _, ev := range sink(line) {
						ev.Offset = lineEndOffset
						emit(ev)
					}
				}
			}
			if err != nil {
				return
			}
		}
	}

	for {
		drain()
		select {
		case <-stop:
			drain() // catch anything written in the gap before stop fired
			return offset
		default:
			time.Sleep(tailPollInterval)
		}
	}
}

// ReplayFile does exactly one bounded "read whatever's on disk right now"
// pass over path starting at fromOffset — used at reconnect time to check
// whether a task's run already produced a result (or any further output)
// while cormake was away, without starting a live tailer. translate is the
// caller's dialect-specific stdout parser (see claude.translateLine/
// cursor.translateLine) — replay has to go through the same dialect the run
// was originally produced by, unlike ProcessAlive which is dialect-free.
func ReplayFile(taskID, path string, fromOffset int64, translate func(taskID string, line []byte) []agent.Event) (events []agent.Event, finalOffset int64) {
	closedStop := make(chan struct{})
	close(closedStop)
	sink := func(line []byte) []agent.Event { return translate(taskID, line) }
	finalOffset = TailFile(closedStop, path, fromOffset, sink, func(ev agent.Event) {
		events = append(events, ev)
	})
	return events, finalOffset
}

// ProcessAlive reports whether pid refers to a live process, via signal 0
// (POSIX: delivers no signal, just checks existence/permission). This only
// proves *some* process currently has this PID — after cormake has been
// closed a long time, a reused PID is a theoretical (if practically
// vanishingly unlikely, for a personal single-user tool run at human pace)
// false positive.
func ProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return syscall.Kill(pid, 0) == nil
}
