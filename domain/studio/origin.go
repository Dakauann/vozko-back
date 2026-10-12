package studio

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

const MaxOriginThreadRunes = 64

type Origin struct {
	ThreadID  string
	CommandID string
}

func (o Origin) Empty() bool {
	return o.ThreadID == "" && o.CommandID == ""
}

func (o Origin) validate() error {
	if o.Empty() {
		return nil
	}
	if o.ThreadID == "" || o.CommandID == "" {
		return issue(FieldOrigin, CodeRequired)
	}
	if utf8.RuneCountInString(o.ThreadID) > MaxOriginThreadRunes || strings.IndexFunc(o.ThreadID, unicode.IsSpace) >= 0 {
		return issue(FieldOrigin, CodeInvalid)
	}
	if _, err := uuid.Parse(o.CommandID); err != nil {
		return issue(FieldOrigin, CodeInvalid)
	}
	return nil
}

func (p *Project) SetOrigin(o Origin) error {
	clean := Origin{ThreadID: strings.TrimSpace(o.ThreadID), CommandID: strings.TrimSpace(o.CommandID)}
	if err := clean.validate(); err != nil {
		return err
	}
	p.Origin = clean
	return nil
}
