package shared

import "math"

const (
	DefaultPageSize = 20
	MaxPageSize     = 100
)

type Pagination struct {
	Page     int
	PageSize int
}

type SortDirection string

const (
	SortAsc  SortDirection = "asc"
	SortDesc SortDirection = "desc"
)

type Sort struct {
	Field     string
	Direction SortDirection
}

type FilterOperator string

const (
	FilterOpEquals  FilterOperator = "eq"
	FilterOpLike    FilterOperator = "like"
	FilterOpIn      FilterOperator = "in"
	FilterOpGte     FilterOperator = "gte"
	FilterOpLte     FilterOperator = "lte"
	FilterOpBetween FilterOperator = "between"
)

type Filter struct {
	Field    string
	Operator FilterOperator
	Values   []string
}

type QueryOptions struct {
	Pagination Pagination
	Filters    []Filter
	Sorts      []Sort
}

type PaginatedResult[T any] struct {
	Items      []T   `json:"items"`
	Page       int   `json:"page"`
	PageSize   int   `json:"page_size"`
	TotalItems int64 `json:"total_items"`
	TotalPages int   `json:"total_pages"`
}

func NormalizePagination(p Pagination) Pagination {
	return NormalizePaginationWithin(p, MaxPageSize)
}

func NormalizePaginationWithin(p Pagination, max int) Pagination {
	page := p.Page
	if page < 1 {
		page = 1
	}

	pageSize := p.PageSize
	if pageSize <= 0 {
		pageSize = DefaultPageSize
	}
	if pageSize > max {
		pageSize = max
	}

	return Pagination{Page: page, PageSize: pageSize}
}

func (p Pagination) Offset() int {
	return p.OffsetWithin(MaxPageSize)
}

func (p Pagination) OffsetWithin(max int) int {
	norm := NormalizePaginationWithin(p, max)
	return (norm.Page - 1) * norm.PageSize
}

func NewPaginatedResult[T any](items []T, pagination Pagination, totalItems int64) *PaginatedResult[T] {
	return NewPaginatedResultWithin(items, pagination, totalItems, MaxPageSize)
}

func NewPaginatedResultWithin[T any](items []T, pagination Pagination, totalItems int64, max int) *PaginatedResult[T] {
	norm := NormalizePaginationWithin(pagination, max)
	totalPages := 0
	if totalItems > 0 {
		totalPages = int(math.Ceil(float64(totalItems) / float64(norm.PageSize)))
	}

	return &PaginatedResult[T]{
		Items:      items,
		Page:       norm.Page,
		PageSize:   norm.PageSize,
		TotalItems: totalItems,
		TotalPages: totalPages,
	}
}
