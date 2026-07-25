package issues_repository

import (
	"gorm.io/gorm"

	"vozko/domain/issues/issue_response"
	"vozko/infra/database/schema"
)

type responseRepository struct {
	db *gorm.DB
}

func NewIssueResponseRepository(db *gorm.DB) issue_response.Repository {
	return &responseRepository{db: db}
}

func (r *responseRepository) Create(resp *issue_response.IssueResponse) error {
	model := &schema.IssueResponse{
		ID:        resp.ID,
		IssueID:   resp.IssueID,
		AuthorID:  resp.AuthorID,
		Body:      resp.Body,
		ImageURL:  resp.ImageURL,
		CreatedAt: resp.CreatedAt,
	}
	return r.db.Create(model).Error
}

func (r *responseRepository) ListByIssueID(issueID string) ([]*issue_response.IssueResponse, error) {
	var records []schema.IssueResponse
	if err := r.db.Where("issue_id = ?", issueID).Order("created_at ASC").Find(&records).Error; err != nil {
		return nil, err
	}

	items := make([]*issue_response.IssueResponse, 0, len(records))
	for i := range records {
		items = append(items, &issue_response.IssueResponse{
			ID:        records[i].ID,
			IssueID:   records[i].IssueID,
			AuthorID:  records[i].AuthorID,
			Body:      records[i].Body,
			ImageURL:  records[i].ImageURL,
			CreatedAt: records[i].CreatedAt,
		})
	}
	return items, nil
}

var _ issue_response.Repository = (*responseRepository)(nil)
