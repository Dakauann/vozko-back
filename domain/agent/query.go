package agent

import (
	"time"

	"vozko/domain/shared"
)

type ListAgentsInput struct {
	WorkspaceID   string
	DepartmentIDs []string
	Search        string
	Tags          []string
	IsActive      *bool
	Archived      *bool
	CreatedAfter  *time.Time
	CreatedBefore *time.Time
	Options       shared.QueryOptions
}
