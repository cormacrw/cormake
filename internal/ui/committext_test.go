package ui

import "testing"

func TestParseAgentResult(t *testing.T) {
	t.Run("extracts commit block and keeps prose summary", func(t *testing.T) {
		text := "Added commit description parsing.\n\n" + commitDescriptionFence + "\n" +
			"- Parse fenced commit block\n" +
			"- Use bullets in git commits\n" +
			"```"
		summary, commitDesc := parseAgentResult(text)
		if want := "Added commit description parsing."; summary != want {
			t.Errorf("summary = %q, want %q", summary, want)
		}
		if want := "- Parse fenced commit block\n- Use bullets in git commits"; commitDesc != want {
			t.Errorf("commitDesc = %q, want %q", commitDesc, want)
		}
	})

	t.Run("no block returns whole text as summary", func(t *testing.T) {
		text := "Implemented the feature with no commit block."
		summary, commitDesc := parseAgentResult(text)
		if summary != text {
			t.Errorf("summary = %q, want %q", summary, text)
		}
		if commitDesc != "" {
			t.Errorf("commitDesc = %q, want empty", commitDesc)
		}
	})

	t.Run("empty commit block is ignored", func(t *testing.T) {
		text := "Summary only.\n\n" + commitDescriptionFence + "\n```"
		summary, commitDesc := parseAgentResult(text)
		if summary != text {
			t.Errorf("summary = %q, want %q", summary, text)
		}
		if commitDesc != "" {
			t.Errorf("commitDesc = %q, want empty", commitDesc)
		}
	})
}

func TestCommitBodyFromTask(t *testing.T) {
	t.Run("prefers commit description", func(t *testing.T) {
		got := commitBodyFromTask("long user summary", "- short bullet")
		if want := "- short bullet"; got != want {
			t.Errorf("commitBodyFromTask() = %q, want %q", got, want)
		}
	})

	t.Run("falls back to summary", func(t *testing.T) {
		got := commitBodyFromTask("fallback summary", "")
		if want := "fallback summary"; got != want {
			t.Errorf("commitBodyFromTask() = %q, want %q", got, want)
		}
	})
}
