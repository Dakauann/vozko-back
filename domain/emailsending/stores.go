package emailsending

import (
	"context"
	"errors"
	"strings"
)

var (
	ErrRawMessageKey      = errors.New("emailsending: raw message key outside the inbound prefix")
	ErrRawMessageTooLarge = errors.New("emailsending: raw message too large")
	ErrMessageNotFound    = errors.New("emailsending: message not found")
	ErrSenderExists       = errors.New("emailsending: sender already exists on the domain")
)

const (
	InboundObjectPrefix = "inbound/"
	MaxRawMessageBytes  = 40 << 20
	MessageIDTag        = "message_id"
)

type AppendedEvent struct {
	New     bool
	Message Message
	Effect  EventEffect
}

type RawMessageStore interface {
	Fetch(ctx context.Context, objectKey string) ([]byte, error)
	Delete(ctx context.Context, objectKey string) error
}

func ValidRawMessageKey(objectKey string) bool {
	rest, ok := strings.CutPrefix(objectKey, InboundObjectPrefix)
	if !ok || rest == "" {
		return false
	}
	return !strings.Contains(rest, "..") && !strings.HasPrefix(rest, "/")
}
