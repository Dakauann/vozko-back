package notification

import (
	"encoding/base64"
	"errors"
	"strings"
	"time"
)

var ErrReadStatusUnknown = errors.New("notification: the read status must be all, read or unread")

type BroadcastReach struct {
	Workspaces int
	Recipients int
	People     int
	Emails     int
}

type BroadcastStats struct {
	Recipients int
	Read       int
}

func (s BroadcastStats) ReadRate() float64 {
	if s.Recipients == 0 {
		return 0
	}
	return float64(s.Read) / float64(s.Recipients)
}

type ReadStatus string

const (
	ReadStatusAll    ReadStatus = "all"
	ReadStatusRead   ReadStatus = "read"
	ReadStatusUnread ReadStatus = "unread"
)

func ParseReadStatus(raw string) (ReadStatus, error) {
	switch ReadStatus(strings.TrimSpace(raw)) {
	case "", ReadStatusAll:
		return ReadStatusAll, nil
	case ReadStatusRead:
		return ReadStatusRead, nil
	case ReadStatusUnread:
		return ReadStatusUnread, nil
	}
	return "", ErrReadStatusUnknown
}

type BroadcastRecipient struct {
	NotificationID string
	UserID         string
	Name           string
	Email          string
	WorkspaceID    string
	WorkspaceName  string
	ReadAt         *time.Time
}

type RecipientQuery struct {
	Status ReadStatus
	After  RecipientCursor
	Paged  bool
	Limit  int
}

type RecipientCursor struct {
	NotificationID string
	UserID         string
}

const recipientCursorSeparator = "|"

func (c RecipientCursor) Encode() string {
	return base64.RawURLEncoding.EncodeToString([]byte(c.NotificationID + recipientCursorSeparator + c.UserID))
}

func ParseRecipientCursor(raw string) (RecipientCursor, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return RecipientCursor{}, false, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return RecipientCursor{}, false, ErrCursorInvalid
	}
	notificationID, userID, found := strings.Cut(string(decoded), recipientCursorSeparator)
	if !found || notificationID == "" || userID == "" {
		return RecipientCursor{}, false, ErrCursorInvalid
	}
	return RecipientCursor{NotificationID: notificationID, UserID: userID}, true, nil
}

type RecipientPage struct {
	Items []BroadcastRecipient
	Next  string
}

func RecipientPageOf(rows []BroadcastRecipient, limit int) RecipientPage {
	if limit <= 0 || len(rows) <= limit {
		return RecipientPage{Items: rows}
	}
	shown := rows[:limit]
	last := shown[len(shown)-1]
	return RecipientPage{Items: shown, Next: RecipientCursor{NotificationID: last.NotificationID, UserID: last.UserID}.Encode()}
}
