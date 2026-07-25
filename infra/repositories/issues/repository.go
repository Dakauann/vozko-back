package issues_repository

import (
	"encoding/json"
	"strings"
	"time"

	"gorm.io/gorm"

	"vozko/domain/issues"
	"vozko/domain/shared"
	"vozko/infra/database/schema"
)

type repository struct {
	db *gorm.DB
}

func NewIssuesRepository(db *gorm.DB) issues.Repository {
	return &repository{db: db}
}

func (r *repository) Create(issue *issues.Issue) error {
	model := toSchema(issue)
	return r.db.Create(&model).Error
}

func (r *repository) GetByID(id string) (*issues.Issue, error) {
	var model schema.Issue
	if err := r.db.Where("id = ?", id).First(&model).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, issues.ErrIssueNotFound
		}
		return nil, err
	}
	return toDomain(&model), nil
}

func (r *repository) List(input issues.ListIssuesInput) (*shared.PaginatedResult[*issues.Issue], error) {
	pagination := shared.NormalizePagination(input.Options.Pagination)

	base := r.db.Model(&schema.Issue{}).Where("workspace_id = ?", input.WorkspaceID)
	base = applyFilters(base, input.Options.Filters)

	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, err
	}

	dataQuery := base.
		Offset(pagination.Offset()).
		Limit(pagination.PageSize)

	dataQuery = applySorts(dataQuery, input.Options.Sorts)

	var records []schema.Issue
	if err := dataQuery.Find(&records).Error; err != nil {
		return nil, err
	}

	items := make([]*issues.Issue, 0, len(records))
	for i := range records {
		items = append(items, toDomain(&records[i]))
	}

	return shared.NewPaginatedResult(items, pagination, total), nil
}

func (r *repository) ListAll(options shared.QueryOptions) (*shared.PaginatedResult[*issues.Issue], error) {
	pagination := shared.NormalizePagination(options.Pagination)

	base := r.db.Model(&schema.Issue{})
	base = applyFilters(base, options.Filters)

	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, err
	}

	dataQuery := base.
		Offset(pagination.Offset()).
		Limit(pagination.PageSize)

	dataQuery = applySorts(dataQuery, options.Sorts)

	var records []schema.Issue
	if err := dataQuery.Find(&records).Error; err != nil {
		return nil, err
	}

	items := make([]*issues.Issue, 0, len(records))
	for i := range records {
		items = append(items, toDomain(&records[i]))
	}

	return shared.NewPaginatedResult(items, pagination, total), nil
}

func (r *repository) UpdateStatus(id string, newStatus issues.IssueStatus) error {
	updates := map[string]interface{}{
		"status":     string(newStatus),
		"updated_at": time.Now().Unix(),
	}
	if newStatus == issues.IssueStatusClosed {
		now := time.Now().Unix()
		updates["closed_at"] = &now
	}

	result := r.db.Model(&schema.Issue{}).Where("id = ?", id).Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return issues.ErrIssueNotFound
	}
	return nil
}

func applyFilters(q *gorm.DB, filters []shared.Filter) *gorm.DB {
	for _, f := range filters {
		switch strings.ToLower(f.Field) {
		case "status":
			if len(f.Values) == 1 {
				q = q.Where("status = ?", f.Values[0])
			} else if len(f.Values) > 1 {
				q = q.Where("status IN ?", f.Values)
			}
		case "search":
			if len(f.Values) > 0 && f.Values[0] != "" {
				like := "%" + f.Values[0] + "%"
				q = q.Where("(title ILIKE ? OR description ILIKE ?)", like, like)
			}
		}
	}
	return q
}

var allowedSortFields = map[string]string{
	"createdat": "created_at",
	"updatedat": "updated_at",
	"status":    "status",
	"title":     "title",
}

func applySorts(q *gorm.DB, sorts []shared.Sort) *gorm.DB {
	applied := false
	for _, s := range sorts {
		col, ok := allowedSortFields[strings.ToLower(s.Field)]
		if !ok {
			continue
		}
		dir := "ASC"
		if s.Direction == shared.SortDesc {
			dir = "DESC"
		}
		q = q.Order(col + " " + dir)
		applied = true
	}
	if !applied {
		q = q.Order("created_at DESC")
	}
	return q
}

func toDomain(m *schema.Issue) *issues.Issue {
	var imageURLs []string
	if m.ImageURLs != nil && *m.ImageURLs != "" {
		_ = json.Unmarshal([]byte(*m.ImageURLs), &imageURLs)
	}
	return &issues.Issue{
		ID:          m.ID,
		WorkspaceID: m.WorkspaceID,
		Title:       m.Title,
		Description: m.Description,
		ImageURLs:   imageURLs,
		Status:      issues.IssueStatus(m.Status),
		CreatedAt:   m.CreatedAt,
		UpdatedAt:   m.UpdatedAt,
		ClosedAt:    m.ClosedAt,
	}
}

func toSchema(i *issues.Issue) *schema.Issue {
	var imageURLsJSON *string
	if len(i.ImageURLs) > 0 {
		b, _ := json.Marshal(i.ImageURLs)
		s := string(b)
		imageURLsJSON = &s
	}
	return &schema.Issue{
		ID:          i.ID,
		WorkspaceID: i.WorkspaceID,
		Title:       i.Title,
		Description: i.Description,
		ImageURLs:   imageURLsJSON,
		Status:      string(i.Status),
		CreatedAt:   i.CreatedAt,
		UpdatedAt:   i.UpdatedAt,
		ClosedAt:    i.ClosedAt,
	}
}

var _ issues.Repository = (*repository)(nil)
