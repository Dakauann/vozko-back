package conversation

import (
	"errors"
	"strings"
)

const (
	SortOrderRecent  = ""
	SortOrderWaiting = "waiting"
)

var ErrInboxSortUnknown = errors.New("conversation: sort must be \"recent\" or \"waiting\"")

func ParseInboxSort(raw string) (string, error) {
	switch strings.TrimSpace(raw) {
	case "", "recent":
		return SortOrderRecent, nil
	case "waiting":
		return SortOrderWaiting, nil
	}
	return "", ErrInboxSortUnknown
}
