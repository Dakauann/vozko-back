package buttonaction

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"vozko/domain/lead"
	"vozko/domain/shared"
	"vozko/domain/whatsapp/template"
)

const MaxConfirmationLength = 1000

type TemplateRef struct {
	Name     string `json:"name"`
	Language string `json:"language"`
}

func RefOf(t *template.Template) TemplateRef {
	if t == nil {
		return TemplateRef{}
	}
	return NewTemplateRef(t.Name, t.Language)
}

func NewTemplateRef(name, language string) TemplateRef {
	key, lang := template.LookupKey(name, language)
	return TemplateRef{Name: key, Language: lang}
}

func (r TemplateRef) Empty() bool {
	return r.Name == "" || r.Language == ""
}

type Params struct {
	Purpose      string `json:"purpose,omitempty"`
	DepartmentID string `json:"departmentId,omitempty"`
	UserID       string `json:"userId,omitempty"`
	AgentID      string `json:"agentId,omitempty"`
	WorkflowID   string `json:"workflowId,omitempty"`
	CallListID   string `json:"callListId,omitempty"`
	FieldKey     string `json:"fieldKey,omitempty"`
	Option       string `json:"option,omitempty"`
	PipelineID   string `json:"pipelineId,omitempty"`
	StageID      string `json:"stageId,omitempty"`
}

func (p Params) trimmed() Params {
	return Params{
		Purpose:      lead.NormalizeConsentPurpose(p.Purpose),
		DepartmentID: strings.TrimSpace(p.DepartmentID),
		UserID:       strings.TrimSpace(p.UserID),
		AgentID:      strings.TrimSpace(p.AgentID),
		WorkflowID:   strings.TrimSpace(p.WorkflowID),
		CallListID:   strings.TrimSpace(p.CallListID),
		FieldKey:     strings.TrimSpace(p.FieldKey),
		Option:       strings.TrimSpace(p.Option),
		PipelineID:   strings.TrimSpace(p.PipelineID),
		StageID:      strings.TrimSpace(p.StageID),
	}
}

type ButtonAction struct {
	Template     TemplateRef `json:"template"`
	ButtonIndex  int         `json:"buttonIndex"`
	ButtonText   string      `json:"buttonText"`
	Kind         Kind        `json:"kind"`
	Params       Params      `json:"params"`
	Confirmation string      `json:"confirmation,omitempty"`
	Version      int64       `json:"version"`
	UpdatedBy    string      `json:"updatedBy,omitempty"`
	UpdatedAt    time.Time   `json:"updatedAt"`
}

type Input struct {
	ButtonIndex  int
	ButtonText   string
	Kind         Kind
	Params       Params
	Confirmation string
}

func NewButtonAction(t *template.Template, in Input) (ButtonAction, error) {
	if t == nil {
		return ButtonAction{}, ErrTemplateNotFound
	}
	reply, ok := quickReplyAt(t, in.ButtonIndex)
	if !ok {
		return ButtonAction{}, ErrButtonNotQuickReply
	}
	action := ButtonAction{
		Template:     RefOf(t),
		ButtonIndex:  in.ButtonIndex,
		ButtonText:   reply.Text,
		Kind:         in.Kind,
		Params:       in.Params.trimmed(),
		Confirmation: strings.TrimSpace(in.Confirmation),
	}
	if err := action.Validate(t); err != nil {
		return ButtonAction{}, err
	}
	return action, nil
}

func BindButtonAction(t *template.Template, in Input, stored []ButtonAction) (ButtonAction, error) {
	if t == nil {
		return ButtonAction{}, ErrTemplateNotFound
	}
	reply, isQuickReply := quickReplyAt(t, in.ButtonIndex)
	if isQuickReply && sameButtonText(reply.Text, in.ButtonText) {
		return NewButtonAction(t, in)
	}
	if kept, ok := untouchedStale(t, in, stored); ok {
		return kept, nil
	}
	if !isQuickReply {
		return ButtonAction{}, ErrButtonNotQuickReply
	}
	return ButtonAction{}, fmt.Errorf("%w: button %d now reads %q", ErrButtonActionStale, in.ButtonIndex, reply.Text)
}

func untouchedStale(t *template.Template, in Input, stored []ButtonAction) (ButtonAction, bool) {
	for _, action := range stored {
		if action.ButtonIndex != in.ButtonIndex || !action.StaleAgainst(t) || !sameButtonText(action.ButtonText, in.ButtonText) {
			continue
		}
		if action.Kind == in.Kind && action.Params == in.Params.trimmed() && action.Confirmation == strings.TrimSpace(in.Confirmation) {
			return action, true
		}
	}
	return ButtonAction{}, false
}

func sameButtonText(text, seen string) bool {
	folded := shared.FoldForMatch(seen)
	return folded != "" && shared.FoldForMatch(text) == folded
}

func (a ButtonAction) Validate(t *template.Template) error {
	if t == nil {
		return ErrTemplateNotFound
	}
	if !a.Kind.Known() {
		return fmt.Errorf("%w: %q", ErrKindUnknown, a.Kind)
	}
	if _, ok := quickReplyAt(t, a.ButtonIndex); !ok {
		return ErrButtonNotQuickReply
	}
	if utf8.RuneCountInString(a.Confirmation) > MaxConfirmationLength {
		return ErrConfirmationTooLong
	}
	return validateParams(a.Kind, a.Params)
}

func (a ButtonAction) StaleAgainst(t *template.Template) bool {
	reply, ok := quickReplyAt(t, a.ButtonIndex)
	return !ok || shared.FoldForMatch(reply.Text) != shared.FoldForMatch(a.ButtonText)
}

func quickReplyAt(t *template.Template, index int) (template.QuickReply, bool) {
	for _, reply := range t.QuickReplies() {
		if reply.Index == index {
			return reply, true
		}
	}
	return template.QuickReply{}, false
}

func validateParams(kind Kind, p Params) error {
	complete, own := paramShape(kind, p)
	if !complete || p != own {
		return ErrParamsInvalid
	}
	return nil
}

func paramShape(kind Kind, p Params) (bool, Params) {
	switch kind {
	case KindOptOut:
		return true, Params{}
	case KindConsentGrant:
		return lead.ValidateConsentPurpose(p.Purpose) == nil, Params{Purpose: p.Purpose}
	case KindHandOff:
		return (p.DepartmentID == "") != (p.UserID == ""), Params{DepartmentID: p.DepartmentID, UserID: p.UserID}
	case KindDelegate:
		return (p.AgentID == "") != (p.WorkflowID == ""), Params{AgentID: p.AgentID, WorkflowID: p.WorkflowID}
	case KindStartWorkflow:
		return p.WorkflowID != "", Params{WorkflowID: p.WorkflowID}
	case KindCallList:
		return p.CallListID != "", Params{CallListID: p.CallListID}
	case KindClassify:
		return p.FieldKey != "" && p.Option != "", Params{FieldKey: p.FieldKey, Option: p.Option}
	case KindCreateDeal:
		return p.PipelineID != "" && p.StageID != "", Params{PipelineID: p.PipelineID, StageID: p.StageID}
	default:
		return false, Params{}
	}
}

func EnsureOnePerButton(actions []ButtonAction) error {
	seen := map[int]bool{}
	for _, action := range actions {
		if seen[action.ButtonIndex] {
			return fmt.Errorf("%w: button %d", ErrDuplicateButton, action.ButtonIndex)
		}
		seen[action.ButtonIndex] = true
	}
	return nil
}
