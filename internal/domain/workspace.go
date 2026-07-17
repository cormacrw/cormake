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
	CreatedAt    time.Time
	UpdatedAt    time.Time
}
