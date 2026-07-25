package schema

type IssueResponse struct {
	ID        string  `gorm:"primaryKey;size:36"`
	IssueID   string  `gorm:"not null;size:36;index"`
	AuthorID  string  `gorm:"not null;size:36;index"`
	Body      string  `gorm:"not null;size:2000"`
	ImageURL  *string `gorm:"size:512"`
	CreatedAt int64   `gorm:"autoCreateTime"`
}

func (IssueResponse) TableName() string {
	return "issue_responses"
}
