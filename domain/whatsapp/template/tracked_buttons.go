package template

import (
	"errors"
	"strings"

	"vozko/domain/choicestats"
)

var (
	ErrTrackedButtonNotURL      = errors.New("only a URL button can track its clicks")
	ErrTrackedButtonDynamic     = errors.New("a URL button whose link has a variable cannot track its clicks")
	ErrTrackedButtonDestination = errors.New("the destination of a tracked button is not a link that can be shortened")
	ErrTrackedLinkUnreadable    = errors.New("the tracked link of this button could not be read, so its destination cannot be restored")
)

type trackedButtonError struct {
	button int
	err    error
}

func (e *trackedButtonError) Error() string { return e.err.Error() }

func (e *trackedButtonError) Unwrap() error { return e.err }

func TrackedButtonRefusal(button int, err error) error {
	if err == nil {
		return nil
	}
	return &trackedButtonError{button: button, err: err}
}

func TrackedButtonIndex(err error) (int, bool) {
	var refusal *trackedButtonError
	if !errors.As(err, &refusal) {
		return 0, false
	}
	return refusal.button, true
}

func TrackedURLButtonsOf(components []TemplateComponent, linkBase string) []int {
	out := []int{}
	for _, comp := range components {
		if strings.ToUpper(strings.TrimSpace(comp.Type)) != "BUTTONS" {
			continue
		}
		for i, btn := range comp.Buttons {
			if !btn.IsURL() {
				continue
			}
			if _, ok := choicestats.TrackedLinkCode(linkBase, btn.URL); ok {
				out = append(out, i)
			}
		}
	}
	return out
}

func (t *Template) TrackedURLButtons(linkBase string) []int {
	if t == nil {
		return []int{}
	}
	return TrackedURLButtonsOf(t.Components, linkBase)
}

func (b TemplateButton) IsURL() bool {
	return strings.ToUpper(strings.TrimSpace(b.Type)) == buttonTypeURL
}
