package domain

import "testing"

func TestSanitizePrefix(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"already clean", "ACME", "ACME"},
		{"lowercase", "acme", "ACME"},
		{"spaces", "My Team", "MYTEAM"},
		{"punctuation", "a-c.m_e!", "ACME"},
		{"digits kept", "team9", "TEAM9"},
		{"empty", "", ""},
		{"no alphanumeric", "!!!", ""},
		{"capped length", "abcdefghijklmnop", "ABCDEFGHIJ"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sanitizePrefix(tc.in); got != tc.want {
				t.Errorf("sanitizePrefix(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestNextDisplayID(t *testing.T) {
	t.Run("increments with explicit prefix", func(t *testing.T) {
		w := &Workspace{Prefix: "ACME"}
		if got, want := w.NextDisplayID(), "ACME-1"; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
		if got, want := w.NextDisplayID(), "ACME-2"; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("falls back to name-derived prefix when Prefix unset", func(t *testing.T) {
		w := &Workspace{Name: "My Team"}
		if got, want := w.NextDisplayID(), "MYTEAM-1"; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("falls back to TASK when both are empty", func(t *testing.T) {
		w := &Workspace{}
		if got, want := w.NextDisplayID(), "TASK-1"; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("falls back to TASK when both sanitize to empty", func(t *testing.T) {
		w := &Workspace{Name: "!!!", Prefix: "---"}
		if got, want := w.NextDisplayID(), "TASK-1"; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
}
