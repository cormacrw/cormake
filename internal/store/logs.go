package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

func (s *Store) logPath(taskID string) string {
	return filepath.Join(s.dir, "logs", taskID+".log")
}

// AppendLogLine appends one entry to a task's persisted log. Unlike
// SaveTask/SaveWorkspaces (single point-in-time JSON snapshots, where a torn
// write would corrupt the whole file), a log is a growing stream of
// independent entries — a plain append is enough, a crash mid-write loses
// at most the tail of the last line, never the file. line may itself
// contain embedded newlines (e.g. an indented multi-line tool_result block);
// that's fine, see LoadLog.
func (s *Store) AppendLogLine(taskID, line string) error {
	f, err := os.OpenFile(s.logPath(taskID), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(line + "\n")
	return err
}

// LoadLog reads a task's persisted log back, or (nil, nil) if it has none
// yet. Returned as a single-element slice rather than split back into
// per-event lines — detail.Model always renders its log via
// strings.Join(lines, "\n"), so the joined output is identical either way
// and there's no need to reconstruct the original entry boundaries.
func (s *Store) LoadLog(taskID string) ([]string, error) {
	data, err := os.ReadFile(s.logPath(taskID))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	text := strings.TrimRight(string(data), "\n")
	if text == "" {
		return nil, nil
	}
	return []string{text}, nil
}
