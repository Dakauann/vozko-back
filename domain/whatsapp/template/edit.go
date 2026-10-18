package template

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	approvedEditsPerDay   = 1
	approvedEditsPerMonth = 10
	editDay               = 24 * time.Hour
	editMonth             = 30 * editDay
	pendingHeaderHandle   = "pending-upload"
)

var (
	ErrTemplateNotEditable = errors.New("whatsapp template: only approved, rejected or paused templates can be edited")
	ErrEditCategoryLocked  = errors.New("whatsapp template: an approved template keeps its category")
	ErrEditUnsupported     = errors.New("whatsapp template: this template has parts that cannot be edited here")
	ErrEditWindow          = errors.New("whatsapp template: Meta allows one edit per day and ten per 30 days on an approved template")
	ErrTemplateInUse       = errors.New("whatsapp template: a running or scheduled campaign uses this template")
	ErrEditNotOwner        = errors.New("whatsapp template: only a workspace with a number on the template's account can edit it")
)

type Edit struct {
	Category       TemplateCategory
	Components     []TemplateComponent
	HeaderMediaURL *string
}

type EditContentInput struct {
	WorkspaceID string
	UserID      string
	TemplateID  string
	Edit        Edit
}

type ContentEditor interface {
	EditContent(ctx context.Context, in EditContentInput) (*Template, error)
}

type EditWindowError struct {
	NextAt time.Time
}

func (e *EditWindowError) Error() string {
	return fmt.Sprintf("%s: next edit at %s", ErrEditWindow, e.NextAt.Format(time.RFC3339))
}

func (e *EditWindowError) Unwrap() error { return ErrEditWindow }

type EditRecord struct {
	TemplateID  string
	WorkspaceID string
	UserID      string
	At          time.Time
	Before      []TemplateComponent
	After       []TemplateComponent
}

type EditLog interface {
	Record(record EditRecord) error
	EditedSince(templateID string, since time.Time) ([]time.Time, error)
}

func EditLookback() time.Duration { return editMonth }

func CheckEdit(t *Template, edit Edit, previous []time.Time, now time.Time) error {
	switch t.Status {
	case TemplateStatusApproved, TemplateStatusRejected, TemplateStatusPaused:
	default:
		return ErrTemplateNotEditable
	}
	if t.IsAuthentication() {
		return ErrEditUnsupported
	}
	category := edit.CategoryFor(t)
	if t.Status == TemplateStatusApproved && category != t.Category {
		return ErrEditCategoryLocked
	}
	if err := ValidateDraft(t.Name, category, withPendingHandle(edit.Components)); err != nil {
		return err
	}
	if t.Status == TemplateStatusApproved {
		return approvedEditWindow(previous, now)
	}
	return nil
}

func (e Edit) CategoryFor(t *Template) TemplateCategory {
	category := TemplateCategory(strings.ToUpper(strings.TrimSpace(string(e.Category))))
	if category == "" {
		return t.Category
	}
	return category
}

func approvedEditWindow(previous []time.Time, now time.Time) error {
	var nextAt time.Time
	inDay, inMonth := 0, 0
	var oldestInMonth time.Time
	for _, at := range previous {
		if now.Sub(at) < editDay {
			inDay++
			nextAt = later(nextAt, at.Add(editDay))
		}
		if now.Sub(at) < editMonth {
			inMonth++
			if oldestInMonth.IsZero() || at.Before(oldestInMonth) {
				oldestInMonth = at
			}
		}
	}
	if inMonth >= approvedEditsPerMonth {
		nextAt = later(nextAt, oldestInMonth.Add(editMonth))
	}
	if inDay >= approvedEditsPerDay || inMonth >= approvedEditsPerMonth {
		return &EditWindowError{NextAt: nextAt}
	}
	return nil
}

func later(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

func withPendingHandle(components []TemplateComponent) []TemplateComponent {
	out := make([]TemplateComponent, len(components))
	for i, comp := range components {
		out[i] = comp
		if !IsMediaHeader(comp) || (comp.Example != nil && len(comp.Example.HeaderHandle) > 0) {
			continue
		}
		example := TemplateExample{}
		if comp.Example != nil {
			example = *comp.Example
		}
		example.HeaderHandle = []string{pendingHeaderHandle}
		out[i].Example = &example
	}
	return out
}

func IsMediaHeader(comp TemplateComponent) bool {
	return strings.EqualFold(strings.TrimSpace(comp.Type), "HEADER") && mediaHeaderFormats[strings.ToUpper(strings.TrimSpace(comp.Format))]
}

func WithHeaderHandle(components []TemplateComponent, handle string) []TemplateComponent {
	out := make([]TemplateComponent, len(components))
	for i, comp := range components {
		out[i] = comp
		if IsMediaHeader(comp) {
			example := TemplateExample{}
			if comp.Example != nil {
				example = *comp.Example
			}
			example.HeaderHandle = []string{handle}
			out[i].Example = &example
		}
	}
	return out
}

func MediaHeaderOf(components []TemplateComponent) (TemplateComponent, bool) {
	for _, comp := range components {
		if IsMediaHeader(comp) {
			return comp, true
		}
	}
	return TemplateComponent{}, false
}
