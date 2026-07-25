package knowledgebase

type KnowledgeBase struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	Description   string  `json:"description"`
	DocumentCount int     `json:"documentCount"`
	SizeMB        int     `json:"sizeMB"`
	LastUpdated   string  `json:"lastUpdated"`
	CreatedAt     string  `json:"createdAt"`
	DeletedAt     *string `json:"deletedAt,omitempty"`
}
