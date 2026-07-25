package buildersession

import "strings"

func (r *ListByWorkflowRequest) Validate() map[string]string {
	r.WorkflowID = strings.TrimSpace(r.WorkflowID)
	return nil
}

func (r *GetSessionRequest) Validate() map[string]string {
	r.SessionID = strings.TrimSpace(r.SessionID)
	return nil
}
