package customerportfolio

import "strings"

type Caller struct {
	UserID  string
	Views   bool
	Uses    bool
	Manages bool
}

func (c Caller) MaySee() error {
	if c.Views || c.Manages {
		return nil
	}
	return ErrForbidden
}

func (c Caller) MayList(holderID string) error {
	if c.Manages || (c.Views && c.isSelf(holderID)) {
		return nil
	}
	return ErrForbidden
}

func (c Caller) MayPlaceFor(holderID string) error {
	if c.Manages || (c.Uses && c.isSelf(holderID)) {
		return nil
	}
	return ErrForbidden
}

func (c Caller) MayRemove(e Entry) error {
	return c.MayPlaceFor(e.HolderID)
}

func (c Caller) MayMove() error {
	if c.Manages {
		return nil
	}
	return ErrForbidden
}

func (c Caller) isSelf(userID string) bool {
	userID = strings.TrimSpace(userID)
	return userID != "" && userID == strings.TrimSpace(c.UserID)
}
