package supportinbox

import "strings"

func (r *CreateSessionRequest) Validate() map[string]string {
	if strings.TrimSpace(r.InboxID) == "" {
		return map[string]string{"inboxId": "required"}
	}
	return nil
}

func (r *ReconnectSessionRequest) Validate() map[string]string {
	if strings.TrimSpace(r.SessionToken) == "" {
		return map[string]string{"sessionToken": "required"}
	}
	return nil
}
