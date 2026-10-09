package shared

import "strings"

type Visibility string

const (
	VisibilityPrivate Visibility = "private"
	VisibilityShared  Visibility = "shared"
)

func (v Visibility) Valid() bool {
	return v == VisibilityPrivate || v == VisibilityShared
}

type Owned struct {
	OwnerID    string
	Visibility Visibility
}

func (o Owned) CanRead(viewerID string) bool {
	if strings.TrimSpace(viewerID) == "" {
		return false
	}
	return o.CanEdit(viewerID) || o.Visibility == VisibilityShared
}

func (o Owned) CanEdit(viewerID string) bool {
	viewer := strings.TrimSpace(viewerID)
	return viewer != "" && viewer == o.OwnerID
}
