package store

import "path/filepath"

// PlanPath is where cormake persists a task's plan when the agent backend
// delivers plan content inline (cursor's createPlanToolCall) rather than
// writing a file the caller can point at directly.
func (s *Store) PlanPath(taskID string) string {
	return filepath.Join(s.dir, "plans", taskID+".md")
}
