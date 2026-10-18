package customerportfolio

import (
	"strings"
	"unicode/utf8"

	"vozko/domain/shared"
)

const (
	DefaultPageSize = shared.DefaultPageSize
	MaxPageSize     = shared.MaxPageSize
	MaxSearchLength = 120
)

type Display struct {
	Channel   shared.EntryType
	Name      string
	Handle    string
	Number    string
	AvatarURL string
}

type Row struct {
	Entry
	Display        Display
	HolderName     string
	AddedByName    string
	HolderReceives bool
}

type ListQuery struct {
	HolderID string
	Search   string
	Page     int
	PageSize int
}

func (q ListQuery) Validate() error {
	if q.Page < 0 || q.PageSize < 0 || utf8.RuneCountInString(strings.TrimSpace(q.Search)) > MaxSearchLength {
		return ErrListQueryInvalid
	}
	return nil
}

func (q ListQuery) Pagination() shared.Pagination {
	return shared.Pagination{Page: q.Page, PageSize: q.PageSize}
}

type Holding struct {
	Subject    Subject
	Entry      *Entry
	HolderName string
}
