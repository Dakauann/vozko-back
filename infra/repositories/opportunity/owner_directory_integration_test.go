package opportunity_repository

import (
	"testing"

	"github.com/google/uuid"
)

func TestOwnerDirectoryKnowsWhoBelongsToTheWorkspace(t *testing.T) {
	db := integrationDB(t)
	for _, stmt := range []string{
		`CREATE TABLE workspace_members (workspace_id uuid NOT NULL, user_id uuid NOT NULL)`,
		`CREATE TABLE agents (id uuid PRIMARY KEY, workspace_id uuid NOT NULL, deleted_at timestamptz)`,
		`CREATE TABLE workflows (id uuid PRIMARY KEY, workspace_id uuid NOT NULL, deleted_at timestamptz)`,
	} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("setup: %v", err)
		}
	}
	ws, otherWS := uuid.New().String(), uuid.New().String()
	member, agent, workflow := uuid.New().String(), uuid.New().String(), uuid.New().String()
	archived, foreignAgent := uuid.New().String(), uuid.New().String()
	db.Exec(`INSERT INTO workspace_members VALUES (?, ?)`, ws, member)
	db.Exec(`INSERT INTO agents VALUES (?, ?, NULL), (?, ?, now()), (?, ?, NULL)`, agent, ws, archived, ws, foreignAgent, otherWS)
	db.Exec(`INSERT INTO workflows VALUES (?, ?, NULL)`, workflow, ws)

	directory := NewOwnerDirectory(db)
	cases := map[string]struct {
		actorID string
		want    bool
	}{
		"member":                    {actorID: member, want: true},
		"stranger":                  {actorID: uuid.New().String(), want: false},
		"agent of the workspace":    {actorID: "ai:" + agent, want: true},
		"archived agent":            {actorID: "ai:" + archived, want: false},
		"agent of another":          {actorID: "ai:" + foreignAgent, want: false},
		"workflow of the workspace": {actorID: "workflow:" + workflow, want: true},
		"member used as an agent":   {actorID: "ai:" + member, want: false},
		"the system":                {actorID: "system", want: false},
		"not an id":                 {actorID: "agent-42", want: false},
		"nobody":                    {actorID: "", want: false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := directory.Belongs(ws, tc.actorID)
			if err != nil {
				t.Fatalf("Belongs(%q) error = %v", tc.actorID, err)
			}
			if got != tc.want {
				t.Fatalf("Belongs(%q) = %v, want %v", tc.actorID, got, tc.want)
			}
		})
	}
}
