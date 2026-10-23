package template

import (
	"strings"

	"vozko/domain/choicestats"
	"vozko/domain/conversation"
	"vozko/domain/whatsappform"
)

type ClickTracking struct {
	LinkBase  string
	PromptKey string
	Tokens    *choicestats.ClickTokens
}

func (c ClickTracking) suffix(buttonIndex int) string {
	token, err := c.Tokens.Issue(buttonIndex, c.PromptKey)
	if err != nil {
		return choicestats.UnattributedClickSuffix
	}
	return token
}

const buttonTypeURL = "URL"

func (t *Template) ChoiceOptions(linkBase string) []choicestats.OfferedOption {
	if t == nil {
		return nil
	}
	tracked, untracked := true, false
	var out []choicestats.OfferedOption
	for _, comp := range t.Components {
		if strings.ToUpper(strings.TrimSpace(comp.Type)) != "BUTTONS" {
			continue
		}
		for i, btn := range comp.Buttons {
			switch strings.ToUpper(strings.TrimSpace(btn.Type)) {
			case ButtonTypeQuickReply:
				out = append(out, choicestats.OfferedOption{ID: choicestats.TemplateButtonOptionID(i), Tracked: &tracked})
			case buttonTypeURL:
				flag := &untracked
				if _, ok := choicestats.TrackedLinkCode(linkBase, btn.URL); ok {
					flag = &tracked
				}
				out = append(out, choicestats.OfferedOption{ID: choicestats.URLOptionID(i), Tracked: flag})
			}
		}
	}
	return out
}

func (t *Template) ShownChoiceIDs(linkBase string) []string {
	var out []string
	for _, option := range t.ChoiceOptions(linkBase) {
		if *option.Tracked {
			out = append(out, option.ID)
		}
	}
	return out
}

func (t *Template) trackedURLParams(tracking ClickTracking) []conversation.TemplateButtonParam {
	var out []conversation.TemplateButtonParam
	for _, i := range TrackedURLButtonsOf(t.Components, tracking.LinkBase) {
		out = append(out, conversation.TemplateButtonParam{
			SubType: conversation.TemplateButtonSubTypeURL,
			Index:   i,
			Text:    tracking.suffix(i),
		})
	}
	return out
}

func (in BilledSendInput) ChoiceSourceFor(templateID string) choicestats.Source {
	if in.ChoiceSource != nil {
		source := *in.ChoiceSource
		source.TemplateID = templateID
		return source
	}
	kind := choicestats.SourceOutreach
	if in.Origin == whatsappform.OriginInbox {
		kind = choicestats.SourceInbox
	}
	return choicestats.Source{Kind: kind, TemplateID: templateID}
}
