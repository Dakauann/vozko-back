package workspaceconfig

import "time"

func rfc3339(at *time.Time) *string {
	if at == nil {
		return nil
	}
	formatted := at.UTC().Format(time.RFC3339)
	return &formatted
}
