package copilot

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	MaxScreenReplyBytes   = 2 << 20
	MaxScreenImageBytes   = 1536 << 10
	MaxScreenImages       = 1
	MaxScreenMessageRunes = 4000
)

var (
	ErrInvalidScreenReply = errors.New("copilot: invalid screen reply")
	ErrNoScreen           = errors.New("copilot: no open screen is attached to this answer")
	ErrScreenUnavailable  = errors.New("copilot: the open screen did not answer in time")
	screenCode            = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	screenImagePrefixes   = []string{"data:image/jpeg;base64,", "data:image/png;base64,"}
)

type ScreenCommandName string

const (
	ScreenRead          ScreenCommandName = "read"
	ScreenEdit          ScreenCommandName = "edit"
	ScreenLook          ScreenCommandName = "look"
	ScreenResolveSource ScreenCommandName = "resolve_source"
	ScreenFollowJob     ScreenCommandName = "follow_job"
)

var screenTimeouts = map[ScreenCommandName]time.Duration{
	ScreenRead:          10 * time.Second,
	ScreenEdit:          30 * time.Second,
	ScreenLook:          45 * time.Second,
	ScreenResolveSource: 10 * time.Second,
	ScreenFollowJob:     10 * time.Second,
}

func ScreenCommandNames() []ScreenCommandName {
	return []ScreenCommandName{ScreenRead, ScreenEdit, ScreenLook, ScreenResolveSource, ScreenFollowJob}
}

func (n ScreenCommandName) Known() bool {
	_, ok := screenTimeouts[n]
	return ok
}

func (n ScreenCommandName) Timeout() time.Duration {
	return screenTimeouts[n]
}

type ScreenCommand struct {
	ID        string            `json:"id"`
	Name      ScreenCommandName `json:"name"`
	ProjectID string            `json:"projectId"`
	Args      interface{}       `json:"args,omitempty"`
}

type ScreenError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ScreenReply struct {
	OK     bool            `json:"ok"`
	Data   json.RawMessage `json:"data,omitempty"`
	Error  *ScreenError    `json:"error,omitempty"`
	Images []string        `json:"images,omitempty"`
}

const ScreenNoEditor = "no_editor"

func (r ScreenReply) Declined() bool {
	return !r.OK && r.Error != nil && r.Error.Code == ScreenNoEditor
}

type Screen interface {
	Run(ctx context.Context, cmd ScreenCommand) (ScreenReply, error)
}

type ScreenMailbox interface {
	Expect(key string) (wait func(ctx context.Context) (ScreenReply, error), cancel func())
	Deliver(key string, reply ScreenReply) error
}

type Scoped interface {
	OfferedOn(view View) bool
}

func (v View) Offers(tool Tool) bool {
	if scoped, ok := tool.(Scoped); ok {
		return scoped.OfferedOn(v)
	}
	return !v.Focused()
}

func (v View) Focused() bool {
	return v.OnStudio()
}

func ScreenKey(threadID, commandID string) string {
	return threadID + ":" + commandID
}

func DecodeScreenReply(raw []byte) (ScreenReply, error) {
	var reply ScreenReply
	if len(raw) > MaxScreenReplyBytes {
		return reply, ErrInvalidScreenReply
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&reply); err != nil || dec.More() {
		return ScreenReply{}, ErrInvalidScreenReply
	}
	if !reply.valid() {
		return ScreenReply{}, ErrInvalidScreenReply
	}
	return reply, nil
}

func (r ScreenReply) valid() bool {
	if r.OK != (r.Error == nil) {
		return false
	}
	if r.Error != nil && (!screenCode.MatchString(r.Error.Code) || utf8.RuneCountInString(r.Error.Message) > MaxScreenMessageRunes) {
		return false
	}
	if len(r.Images) > MaxScreenImages {
		return false
	}
	for _, image := range r.Images {
		if !IsImageDataURL(image, MaxScreenImageBytes) {
			return false
		}
	}
	return true
}

func IsImageDataURL(value string, maxBytes int) bool {
	for _, prefix := range screenImagePrefixes {
		payload, ok := strings.CutPrefix(value, prefix)
		if !ok {
			continue
		}
		if base64.StdEncoding.DecodedLen(len(payload)) > maxBytes+2 {
			return false
		}
		decoded, err := base64.StdEncoding.DecodeString(payload)
		return err == nil && len(decoded) > 0 && len(decoded) <= maxBytes
	}
	return false
}

type ArgumentLogger interface {
	LogsArguments() bool
}
