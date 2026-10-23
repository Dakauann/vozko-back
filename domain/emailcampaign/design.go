package emailcampaign

import (
	"errors"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"vozko/domain/emaildesign"
)

var (
	ErrDesignNotFound     = errors.New("emailcampaign: design not found")
	ErrDesignKindInvalid  = errors.New("emailcampaign: unknown design kind")
	ErrThumbnailInvalid   = errors.New("emailcampaign: the thumbnail must be an https url")
	ErrDesignNameRequired = errors.New("emailcampaign: a design needs a name")
)

type DesignKind string

const (
	DesignTemplate DesignKind = "template"
	DesignCampaign DesignKind = "campaign"
)

func (k DesignKind) Valid() bool {
	return k == DesignTemplate || k == DesignCampaign
}

type Design struct {
	ID           string
	WorkspaceID  string
	Name         string
	Document     emaildesign.Document
	ThumbnailURL string
	Kind         DesignKind
	CreatedBy    string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type DesignInput struct {
	Name         string
	Document     emaildesign.Document
	ThumbnailURL string
	Kind         DesignKind
}

func (in DesignInput) Validate() error {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return ErrDesignNameRequired
	}
	if utf8.RuneCountInString(name) > MaxNameRunes {
		return ErrNameTooLong
	}
	if !in.Kind.Valid() {
		return ErrDesignKindInvalid
	}
	if raw := strings.TrimSpace(in.ThumbnailURL); raw != "" {
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "https" || u.Host == "" {
			return ErrThumbnailInvalid
		}
	}
	if issues := emaildesign.Validate(in.Document); len(issues) > 0 {
		return &emaildesign.ValidationError{Kind: emaildesign.ErrInvalidDocument, Issues: issues}
	}
	return nil
}

func (d *Design) Apply(in DesignInput) {
	d.Name = strings.TrimSpace(in.Name)
	d.Document = in.Document
	d.ThumbnailURL = strings.TrimSpace(in.ThumbnailURL)
	d.Kind = in.Kind
}
