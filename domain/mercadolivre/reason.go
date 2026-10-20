package mercadolivre

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

var (
	ErrReasonUnknown     = errors.New("mercado livre did not offer this reason for the sale")
	ErrReasonUnavailable = errors.New("mercado livre does not allow this reason for the sale right now")
	ErrReasonCapReached  = errors.New("this reason was already used as many times as mercado livre allows")
	ErrReasonContent     = errors.New("a reason takes either one of its templates with every variable, or a text")
	ErrPersonOnly        = errors.New("only a person can do this on a mercado livre conversation")
)

func WithCaps(options []ReasonOption, caps []ReasonCap) []ReasonOption {
	byOption := make(map[string]int, len(caps))
	for _, c := range caps {
		byOption[c.OptionID] = c.CapAvailable
	}
	out := make([]ReasonOption, 0, len(options))
	for _, o := range options {
		if available, ok := byOption[o.ID]; ok {
			o.CapAvailable = available
		}
		out = append(out, o)
	}
	return out
}

func ForSite(options []ReasonOption, siteID string) []ReasonOption {
	site := strings.ToUpper(strings.TrimSpace(siteID))
	out := make([]ReasonOption, 0, len(options))
	for _, o := range options {
		if o.Templates != nil {
			templates := make([]ReasonTemplate, 0, len(o.Templates))
			for _, t := range o.Templates {
				if site != "" {
					t.Text = t.TextsBySite[site]
				}
				t.TextsBySite = nil
				templates = append(templates, t)
			}
			o.Templates = templates
		}
		out = append(out, o)
	}
	return out
}

func ValidateReasonSend(options []ReasonOption, send ReasonSend) error {
	option, ok := findReason(options, send.OptionID)
	if !ok {
		return fmt.Errorf("%w: %q", ErrReasonUnknown, send.OptionID)
	}
	if !option.Enabled || !option.Actionable {
		return fmt.Errorf("%w: %s", ErrReasonUnavailable, option.ID)
	}
	if option.CapAvailable <= 0 {
		return fmt.Errorf("%w: %s", ErrReasonCapReached, option.ID)
	}
	hasTemplate, hasText := send.TemplateID != "", strings.TrimSpace(send.Text) != ""
	switch {
	case hasTemplate && !hasText:
		return validateReasonTemplate(option, send)
	case hasText && !hasTemplate:
		return validateReasonText(option, send.Text)
	}
	return ErrReasonContent
}

func findReason(options []ReasonOption, id string) (ReasonOption, bool) {
	for _, o := range options {
		if o.ID == id {
			return o, true
		}
	}
	return ReasonOption{}, false
}

func validateReasonTemplate(option ReasonOption, send ReasonSend) error {
	for _, tpl := range option.Templates {
		if tpl.ID != send.TemplateID {
			continue
		}
		if len(send.Vars) != len(tpl.Vars) {
			return fmt.Errorf("%w: template %s takes %d variables", ErrReasonContent, tpl.ID, len(tpl.Vars))
		}
		given := make(map[string]struct{}, len(send.Vars))
		for _, v := range send.Vars {
			given[v.ID] = struct{}{}
		}
		for _, v := range tpl.Vars {
			if _, ok := given[v.ID]; !ok {
				return fmt.Errorf("%w: template %s needs %s", ErrReasonContent, tpl.ID, v.ID)
			}
		}
		return nil
	}
	return fmt.Errorf("%w: template %q is not offered for %s", ErrReasonContent, send.TemplateID, option.ID)
}

func validateReasonText(option ReasonOption, text string) error {
	if err := ValidateMessageText(text); err != nil {
		return err
	}
	if n := utf8.RuneCountInString(text); option.CharLimit > 0 && n > option.CharLimit {
		return &TextTooLongError{Max: option.CharLimit, Got: n}
	}
	return nil
}
