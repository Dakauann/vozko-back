package category

import (
	"errors"
	"strings"
	"time"
)

type Category struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Slug        string     `json:"slug"`
	Description string     `json:"description,omitempty"`
	ParentID    *string    `json:"parentId,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
	Parent      *Category  `json:"parent,omitempty"`
	Children    []Category `json:"children,omitempty"`
}

var (
	ErrCategoryNotFound       = errors.New("category not found")
	ErrCategoryNameRequired   = errors.New("category name is required")
	ErrCategorySlugRequired   = errors.New("category slug is required")
	ErrCategorySlugInvalid    = errors.New("category slug is invalid")
	ErrCategorySlugExists     = errors.New("category slug already exists")
	ErrCategoryParentNotFound = errors.New("parent category not found")
	ErrCategoryCycle          = errors.New("category cannot reference itself or its descendants")
	ErrCategoryInUse          = errors.New("category is associated with existing products")
	ErrCategoryHasChildren    = errors.New("category has child categories")
)

func NormalizeSlug(value string) string {
	trimmed := strings.TrimSpace(strings.ToLower(value))
	if trimmed == "" {
		return ""
	}

	var builder strings.Builder
	prevDash := false
	for _, r := range trimmed {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			builder.WriteRune(r)
			prevDash = false
			continue
		}
		if r == ' ' || r == '-' || r == '_' {
			if !prevDash && builder.Len() > 0 {
				builder.WriteRune('-')
				prevDash = true
			}
			continue
		}
	}

	slug := builder.String()
	slug = strings.Trim(slug, "-")
	return slug
}
