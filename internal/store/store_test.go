package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadTemplate(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	const want = "# New task\n\n- [ ] step one\n"
	if err := os.WriteFile(filepath.Join(dir, "template.md"), []byte(want), 0o644); err != nil {
		t.Fatalf("writing template fixture: %v", err)
	}

	got, err := s.ReadTemplate("template.md")
	if err != nil {
		t.Fatalf("ReadTemplate: %v", err)
	}
	if got != want {
		t.Errorf("ReadTemplate content = %q, want %q", got, want)
	}

	if _, err := s.ReadTemplate("missing.md"); err == nil {
		t.Error("ReadTemplate on missing file: expected error, got nil")
	}
}
