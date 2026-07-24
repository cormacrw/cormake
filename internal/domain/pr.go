package domain

import "time"

// PRComment is one entry in a PR's conversation feed — either a plain issue
// comment or a review (Kind holds the review's state, e.g. "approved",
// "changes requested", lowercased and de-underscored from GitHub's own
// enum) — merged together and time-ordered by queryPR (see internal/ui/pr.go)
// since both read the same way in the Comments tab.
type PRComment struct {
	Author    string
	Body      string
	CreatedAt time.Time
	Kind      string
}

// PRSnapshot is a point-in-time read of an open PR's description and
// conversation, fetched via `gh pr view` (see internal/ui/pr.go) — kept
// in-memory only (ui.Model/detail.Model), not persisted alongside Task,
// since it's cheap to re-fetch and would otherwise just go stale on disk
// between polls.
type PRSnapshot struct {
	Number    int
	URL       string
	Title     string
	Body      string
	State     string // GitHub's own enum: OPEN, MERGED, CLOSED
	Comments  []PRComment
	FetchedAt time.Time
}
