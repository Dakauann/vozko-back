package personalinbox

import (
	"errors"
	"strings"
)

var ErrViewUnknown = errors.New("personalinbox: personal must be empty or \"pinned\"")

type View string

const (
	ViewNone   View = ""
	ViewInbox  View = "inbox"
	ViewPinned View = "pinned"
)

func ParseInboxView(raw string) (View, error) {
	switch strings.TrimSpace(raw) {
	case "":
		return ViewInbox, nil
	case string(ViewPinned):
		return ViewPinned, nil
	}
	return ViewNone, ErrViewUnknown
}

func (v View) Applies() bool {
	return v == ViewInbox || v == ViewPinned
}
