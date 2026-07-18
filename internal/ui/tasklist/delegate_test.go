package tasklist

import (
	"bytes"
	"testing"

	"github.com/charmbracelet/bubbles/list"

	"cormake/internal/domain"
)

func renderItem(t *testing.T, d Delegate, task domain.Task) string {
	t.Helper()
	l := list.New([]list.Item{Item{Task: task}}, d, 40, 10)
	var buf bytes.Buffer
	d.Render(&buf, l, 0, Item{Task: task})
	return buf.String()
}

func TestDelegateRenderUsesSpinnerFrameForInFlightStatuses(t *testing.T) {
	frame := "⠋"
	d := Delegate{Frame: &frame}

	for _, status := range []domain.Status{domain.StatusPlanning, domain.StatusInProgress} {
		task := domain.Task{Title: "in flight", Status: status}
		got := renderItem(t, d, task)
		if !bytes.Contains([]byte(got), []byte(frame)) {
			t.Errorf("status %q: rendered line %q does not contain spinner frame %q", status, got, frame)
		}
		_, staticGlyph := task.DisplayStage()
		if bytes.Contains([]byte(got), []byte(staticGlyph)) {
			t.Errorf("status %q: rendered line %q should not contain the static glyph %q once a spinner frame is set", status, got, staticGlyph)
		}
	}
}

func TestDelegateRenderKeepsStaticGlyphForOtherStatuses(t *testing.T) {
	frame := "⠋"
	d := Delegate{Frame: &frame}

	task := domain.Task{Title: "todo", Status: domain.StatusTodo}
	_, wantGlyph := task.DisplayStage()

	got := renderItem(t, d, task)
	if !bytes.Contains([]byte(got), []byte(wantGlyph)) {
		t.Errorf("rendered line %q does not contain static glyph %q", got, wantGlyph)
	}
	if bytes.Contains([]byte(got), []byte(frame)) {
		t.Errorf("rendered line %q should not contain the spinner frame %q for a non-in-flight status", got, frame)
	}
}
