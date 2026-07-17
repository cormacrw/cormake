package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"cormake/internal/domain"
)

type Store struct {
	dir string
}

// Open ensures the store's directory (and its tasks/ and logs/
// subdirectories) exist and returns a Store rooted there.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(dir, "tasks"), 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(dir, "logs"), 0o755); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

func (s *Store) workspacesPath() string {
	return filepath.Join(s.dir, "workspaces.json")
}

// WorkspacesPath exposes the on-disk path to workspaces.json so callers can
// open it directly (e.g. in an external editor for manual repo management).
func (s *Store) WorkspacesPath() string {
	return s.workspacesPath()
}

func (s *Store) taskPath(id string) string {
	return filepath.Join(s.dir, "tasks", id+".json")
}

// LoadWorkspaces reads workspaces.json. A missing file (first run) returns
// (nil, nil) rather than an error, letting the caller decide to seed a
// default workspace.
func (s *Store) LoadWorkspaces() ([]domain.Workspace, error) {
	data, err := os.ReadFile(s.workspacesPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var workspaces []domain.Workspace
	if err := json.Unmarshal(data, &workspaces); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", s.workspacesPath(), err)
	}
	return workspaces, nil
}

// SaveWorkspaces overwrites workspaces.json with the full set — it's one
// small shared index file, not one-per-workspace.
func (s *Store) SaveWorkspaces(workspaces []domain.Workspace) error {
	data, err := json.MarshalIndent(workspaces, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(s.workspacesPath(), data, 0o644)
}

// LoadTasks reads every tasks/*.json file. A single file that fails to
// parse is skipped with a stderr warning rather than aborting the whole
// load — task files are independent, hand-editable units, so one bad file
// shouldn't take down every other task.
func (s *Store) LoadTasks() ([]domain.Task, error) {
	entries, err := os.ReadDir(filepath.Join(s.dir, "tasks"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var tasks []domain.Task
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := filepath.Join(s.dir, "tasks", e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintln(os.Stderr, "cormake: skipping unreadable task file", path+":", err)
			continue
		}
		var t domain.Task
		if err := json.Unmarshal(data, &t); err != nil {
			fmt.Fprintln(os.Stderr, "cormake: skipping corrupt task file", path+":", err)
			continue
		}
		tasks = append(tasks, t)
	}

	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].CreatedAt.Before(tasks[j].CreatedAt)
	})
	return tasks, nil
}

// SaveTask atomically writes t to its own file under tasks/.
func (s *Store) SaveTask(t domain.Task) error {
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(s.taskPath(t.ID), data, 0o644)
}

// DeleteTask removes a task's file from disk, along with its persisted log
// (see logs.go) and raw stdout/stderr/offset files (see offset.go) —
// deleting a task is meant to fully remove it, not leave stray files
// behind. Deleting an already-gone task is not an error — the end state (no
// files on disk) is what's asked for.
func (s *Store) DeleteTask(id string) error {
	err := os.Remove(s.taskPath(id))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	// Best-effort; a missing file is fine.
	_ = os.Remove(s.logPath(id))
	_ = os.Remove(s.RawStdoutPath(id))
	_ = os.Remove(s.RawStderrPath(id))
	_ = os.Remove(s.offsetPath(id))
	return nil
}
