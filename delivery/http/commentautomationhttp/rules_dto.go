package commentautomationhttp

import "time"

type RuleRequest struct {
	Name             string   `json:"name"`
	Enabled          bool     `json:"enabled"`
	ContainerID      string   `json:"containerId"`
	Match            string   `json:"match"`
	Keywords         []string `json:"keywords"`
	Actions          []string `json:"actions"`
	PublicReplyText  string   `json:"publicReplyText"`
	PrivateReplyText string   `json:"privateReplyText"`
	Priority         int      `json:"priority"`
}

type RuleResponse struct {
	ID               string    `json:"id"`
	AccountID        string    `json:"accountId"`
	Source           string    `json:"source"`
	Name             string    `json:"name"`
	Enabled          bool      `json:"enabled"`
	ContainerID      string    `json:"containerId,omitempty"`
	Match            string    `json:"match"`
	Keywords         []string  `json:"keywords"`
	Actions          []string  `json:"actions"`
	PublicReplyText  string    `json:"publicReplyText,omitempty"`
	PrivateReplyText string    `json:"privateReplyText,omitempty"`
	Priority         int       `json:"priority"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

type RuleDeletedResponse struct {
	Status string `json:"status"`
}
