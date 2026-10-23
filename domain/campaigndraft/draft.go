package campaigndraft

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

type Channel string

const (
	ChannelWhatsApp           Channel = "whatsapp"
	ChannelUnofficialWhatsApp Channel = "unofficial_whatsapp"
)

const (
	MaxPayloadBytes = 1 << 20
	MaxNameLength   = 255
	ListLimit       = 100
)

var (
	ErrNotFound   = errors.New("campaign draft: not found")
	ErrInvalid    = errors.New("campaign draft: invalid")
	ErrTooLarge   = errors.New("campaign draft: the draft is larger than 1 MB")
	ErrIncomplete = errors.New("campaign draft: the draft has no campaign request yet")
)

func Channels() []Channel {
	return []Channel{ChannelWhatsApp, ChannelUnofficialWhatsApp}
}

func (c Channel) Valid() bool {
	for _, known := range Channels() {
		if c == known {
			return true
		}
	}
	return false
}

func ParseChannel(value string) (Channel, error) {
	ch := Channel(strings.TrimSpace(value))
	if !ch.Valid() {
		return "", fmt.Errorf("%w: unknown channel %q", ErrInvalid, value)
	}
	return ch, nil
}

type Draft struct {
	ID          string
	WorkspaceID string
	Channel     Channel
	Name        string
	Payload     json.RawMessage
	CreatedBy   string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func New(id, workspaceID, createdBy string, ch Channel, name string, payload json.RawMessage, now time.Time) (Draft, error) {
	if strings.TrimSpace(workspaceID) == "" || strings.TrimSpace(createdBy) == "" {
		return Draft{}, fmt.Errorf("%w: workspace and author are required", ErrInvalid)
	}
	if !ch.Valid() {
		return Draft{}, fmt.Errorf("%w: unknown channel %q", ErrInvalid, ch)
	}
	d := Draft{ID: id, WorkspaceID: strings.TrimSpace(workspaceID), Channel: ch, CreatedBy: strings.TrimSpace(createdBy), CreatedAt: now, UpdatedAt: now}
	if err := d.Revise(name, payload, now); err != nil {
		return Draft{}, err
	}
	return d, nil
}

func (d *Draft) Revise(name string, payload json.RawMessage, now time.Time) error {
	name = strings.TrimSpace(name)
	if utf8.RuneCountInString(name) > MaxNameLength {
		return fmt.Errorf("%w: the name is longer than %d characters", ErrInvalid, MaxNameLength)
	}
	clean, err := objectPayload(payload)
	if err != nil {
		return err
	}
	d.Name = name
	d.Payload = clean
	d.UpdatedAt = now
	return nil
}

func objectPayload(payload json.RawMessage) (json.RawMessage, error) {
	trimmed := bytes.TrimSpace(payload)
	if len(trimmed) == 0 {
		return json.RawMessage(`{}`), nil
	}
	if len(trimmed) > MaxPayloadBytes {
		return nil, ErrTooLarge
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &object); err != nil || object == nil {
		return nil, fmt.Errorf("%w: the payload must be a JSON object", ErrInvalid)
	}
	return json.RawMessage(append([]byte(nil), trimmed...)), nil
}

func (d Draft) Request() (json.RawMessage, error) {
	var parts struct {
		Request json.RawMessage `json:"request"`
	}
	if err := json.Unmarshal(d.Payload, &parts); err != nil {
		return nil, ErrIncomplete
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(parts.Request, &object); err != nil || object == nil {
		return nil, ErrIncomplete
	}
	return parts.Request, nil
}

type Repository interface {
	Create(ctx context.Context, d *Draft) error
	Update(ctx context.Context, d *Draft) error
	Find(ctx context.Context, workspaceID string, ch Channel, id string) (*Draft, error)
	List(ctx context.Context, workspaceID string, ch Channel, limit int) ([]Draft, error)
	Delete(ctx context.Context, workspaceID string, ch Channel, id string) error
}

var errorCodes = []struct {
	err  error
	code string
}{
	{ErrNotFound, "campaign_draft_not_found"},
	{ErrTooLarge, "campaign_draft_too_large"},
	{ErrIncomplete, "campaign_draft_incomplete"},
	{ErrInvalid, "campaign_draft_invalid"},
}

func ErrorCode(err error) string {
	for _, known := range errorCodes {
		if errors.Is(err, known.err) {
			return known.code
		}
	}
	return ""
}
