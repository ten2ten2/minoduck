package tasks

import "github.com/riverqueue/river"

// Only references belong in durable task arguments; never credentials or file bodies.
type Args struct {
	Task        string `json:"task"`
	WorkspaceID string `json:"workspace_id"`
	ResourceID  string `json:"resource_id"`
}

func (Args) Kind() string                 { return "minoduck_task" }
func (Args) InsertOpts() river.InsertOpts { return river.InsertOpts{MaxAttempts: 5} }
