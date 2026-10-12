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
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"

	"vozko/domain/studio"
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
	ErrScreenUnconfirmed  = errors.New("copilot: the open screen started the change and did not confirm it in time")
	ErrInvalidScreenClaim = errors.New("copilot: invalid screen claim")
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
	ScreenExport        ScreenCommandName = "export"
	ScreenListen        ScreenCommandName = "listen"
	ScreenSavePreset    ScreenCommandName = "save_preset"
	ScreenCreateProject ScreenCommandName = "create_project"
)

var screenTimeouts = map[ScreenCommandName]time.Duration{
	ScreenRead:          10 * time.Second,
	ScreenEdit:          30 * time.Second,
	ScreenLook:          45 * time.Second,
	ScreenResolveSource: 10 * time.Second,
	ScreenFollowJob:     10 * time.Second,
	ScreenExport:        60 * time.Second,
	ScreenListen:        45 * time.Second,
	ScreenSavePreset:    20 * time.Second,
	ScreenCreateProject: 45 * time.Second,
}

func ScreenCommandNames() []ScreenCommandName {
	return []ScreenCommandName{ScreenRead, ScreenEdit, ScreenLook, ScreenResolveSource, ScreenFollowJob, ScreenExport, ScreenListen, ScreenSavePreset, ScreenCreateProject}
}

func (n ScreenCommandName) Known() bool {
	_, ok := screenTimeouts[n]
	return ok
}

func (n ScreenCommandName) NeedsProject() bool {
	return n != ScreenCreateProject
}

func (n ScreenCommandName) Timeout() time.Duration {
	return screenTimeouts[n]
}

func (n ScreenCommandName) Commits() bool {
	return n == ScreenEdit || n == ScreenSavePreset || n == ScreenCreateProject
}

type ScreenGo struct {
	ID       string `json:"id"`
	Claimant string `json:"claimant"`
}

func ScreenClaim(claimant string) (ScreenReply, error) {
	if _, err := uuid.Parse(claimant); err != nil || len(claimant) != len(uuid.Nil.String()) {
		return ScreenReply{}, ErrInvalidScreenClaim
	}
	return ScreenReply{OK: true, Claim: claimant}, nil
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
	Target *ScreenTarget   `json:"target,omitempty"`
	Claim  string          `json:"claim,omitempty"`
}

const (
	LookArtboard  = "artboard"
	LookArtboards = "artboards"
	LookMoments   = "moments"
	LookMedia     = "media"

	maxScreenTargetIDRunes = 128
	maxArtboardNumber      = 100_000
)

type ScreenTarget struct {
	Kind       string `json:"kind"`
	ProjectID  string `json:"projectId,omitempty"`
	ArtboardID string `json:"artboardId,omitempty"`
	N          int    `json:"n,omitempty"`
	Name       string `json:"name,omitempty"`
	FromMS     *int64 `json:"fromMs,omitempty"`
	ToMS       *int64 `json:"toMs,omitempty"`
}

func plainText(value string, maxRunes int) bool {
	if utf8.RuneCountInString(value) > maxRunes || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func (t ScreenTarget) valid() bool {
	if t.ProjectID != "" || !plainText(t.ArtboardID, maxScreenTargetIDRunes) || !plainText(t.Name, studio.MaxNameRunes) {
		return false
	}
	artboard := t.ArtboardID != "" || t.N != 0 || t.Name != ""
	timed := t.FromMS != nil || t.ToMS != nil
	switch t.Kind {
	case LookArtboard:
		return !timed && strings.TrimSpace(t.ArtboardID) != "" && t.N >= 1 && t.N <= maxArtboardNumber
	case LookMoments:
		return !artboard && t.FromMS != nil && t.ToMS != nil && *t.FromMS >= 0 && *t.FromMS <= *t.ToMS && *t.ToMS <= studio.MaxVideoMS
	case LookArtboards, LookMedia:
		return !artboard && !timed
	}
	return false
}

func (t ScreenTarget) In(projectID string) *ScreenTarget {
	t.ProjectID = projectID
	return &t
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
	if r.OK != (r.Error == nil) || r.Claim != "" {
		return false
	}
	if r.Error != nil && (!screenCode.MatchString(r.Error.Code) || utf8.RuneCountInString(r.Error.Message) > MaxScreenMessageRunes) {
		return false
	}
	if len(r.Images) > MaxScreenImages {
		return false
	}
	if r.Target != nil && (!r.OK || !r.Target.valid()) {
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
