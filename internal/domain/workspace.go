package domain

import "time"

type Repo struct {
	ID      string
	Name    string
	Path    string
	AddedAt time.Time
}

type Workspace struct {
	ID           string
	Name         string
	Repos        []Repo
	PrimaryColor string

	// Prefix is the user-set readable-ID prefix for this workspace's tasks,
	// e.g. "ACME" -> task IDs like "ACME-7". Hand-edited via workspaces.json
	// (there's no dedicated settings UI yet, same as PrimaryColor). When
	// unset, NextDisplayID derives one from Name instead.
	Prefix string

	// NextTaskNumber is the next sequence number NextDisplayID will hand
	// out for Prefix. It does not reset when Prefix is edited by hand.
	NextTaskNumber int

	CreatedAt time.Time
	UpdatedAt time.Time
}
