package store

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func (s *Store) offsetPath(taskID string) string {
	return filepath.Join(s.dir, "logs", taskID+".offset")
}

// RawStdoutPath/RawStderrPath are where a task's live or most-recently-run
// claude process has its stdout/stderr redirected (see agent.RunSpec) — a
// real file, not a pipe, so it can keep being written to whether or not
// cormake is around to read it. Owned here (not computed ad hoc by
// internal/ui) so the logs/ directory layout stays centralized.
func (s *Store) RawStdoutPath(taskID string) string {
	return filepath.Join(s.dir, "logs", taskID+".raw.jsonl")
}

func (s *Store) RawStderrPath(taskID string) string {
	return filepath.Join(s.dir, "logs", taskID+".raw.stderr")
}

// SaveOffset persists how far into a task's raw stdout file (see
// RawStdoutPath) has already been translated and appended to its display
// log — so a later Attach/replay resumes from there instead of re-emitting
// duplicate display-log lines. Mirrors AppendLogLine's atomic-not-required
// reasoning in spirit, but this value is overwritten wholesale each time
// (not appended), so it goes through writeFileAtomic like SaveTask.
func (s *Store) SaveOffset(taskID string, offset int64) error {
	return writeFileAtomic(s.offsetPath(taskID), []byte(strconv.FormatInt(offset, 10)), 0o644)
}

// LoadOffset reads a task's persisted offset back. A missing or
// unparseable file degrades to 0 rather than an error — the worst case is
// re-translating a few already-shown lines on reattach, not a crash.
func (s *Store) LoadOffset(taskID string) (int64, error) {
	data, err := os.ReadFile(s.offsetPath(taskID))
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	offset, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return 0, nil
	}
	return offset, nil
}
