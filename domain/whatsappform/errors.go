package whatsappform

import (
	"errors"
	"fmt"
	"strings"
)

var (
	ErrWorkspaceRequired      = errors.New("whatsappform: workspace is required")
	ErrForbidden              = errors.New("whatsappform: not allowed")
	ErrFormNotFound           = errors.New("whatsappform: form not found")
	ErrFormArchived           = errors.New("whatsappform: the form is archived")
	ErrNameRequired           = errors.New("whatsappform: the form needs a name")
	ErrNameTooLong            = errors.New("whatsappform: the form name is too long")
	ErrNameTaken              = errors.New("whatsappform: another form already has this name")
	ErrStaleDraft             = errors.New("whatsappform: the draft changed since it was opened")
	ErrVersionNotFound        = errors.New("whatsappform: version not found")
	ErrDocumentInvalid        = errors.New("whatsappform: the form has problems to fix before it can be published")
	ErrNotOwner               = errors.New("whatsappform: only the workspace that owns the number can use forms on it")
	ErrProviderUnsupported    = errors.New("whatsappform: forms are not available for this number's provider")
	ErrNoEligibleAccount      = errors.New("whatsappform: the workspace owns no connected official number to publish on")
	ErrNoPublication          = errors.New("whatsappform: the form is not published on this number's WhatsApp account")
	ErrPublicationNotSendable = errors.New("whatsappform: Meta is not delivering this form right now")
	ErrPublicationNotFound    = errors.New("whatsappform: publication not found")
	ErrOutsideWindow          = errors.New("whatsappform: the 24 hour window is closed; send a template with this form instead")
	ErrChannelUnsupported     = errors.New("whatsappform: forms are sent only on official WhatsApp conversations")
	ErrTokenRequired          = errors.New("whatsappform: a form send needs its own token")
	ErrTokenInvalid           = errors.New("whatsappform: the form token is not one Vozko issued")
	ErrSendNotFound           = errors.New("whatsappform: send not found")
	ErrResponseNotFound       = errors.New("whatsappform: response not found")
	ErrResponseInvalid        = errors.New("whatsappform: the form answer could not be read")
	ErrCopyInvalid            = errors.New("whatsappform: the message text of the form is invalid")
	ErrListQueryInvalid       = errors.New("whatsappform: the list query is invalid")
	ErrFormInUse              = errors.New("whatsappform: a running or scheduled send still uses this form")
	ErrImportUnsupported      = errors.New("whatsappform: this Meta flow cannot be imported")
	ErrStarterUnknown         = errors.New("whatsappform: there is no starter with that key")
	ErrDescriptionTooLong     = errors.New("whatsappform: the form description is too long")
)

type Problem struct {
	Screen   string `json:"screen,omitempty"`
	Block    string `json:"block,omitempty"`
	Property string `json:"property,omitempty"`
	Code     string `json:"code"`
	Limit    int    `json:"limit,omitempty"`
	Message  string `json:"message,omitempty"`
}

const (
	ProblemRequired         = "required"
	ProblemTooLong          = "too_long"
	ProblemTooMany          = "too_many"
	ProblemTooFew           = "too_few"
	ProblemDuplicate        = "duplicate"
	ProblemInvalid          = "invalid"
	ProblemNotAllowed       = "not_allowed"
	ProblemUnknownTarget    = "unknown_target"
	ProblemBackwardRoute    = "backward_route"
	ProblemUnreachable      = "unreachable"
	ProblemNoInput          = "no_input"
	ProblemBindingUnknown   = "binding_unknown_field"
	ProblemBindingKind      = "binding_incompatible"
	ProblemBindingSensitive = "binding_sensitive"
	ProblemBindingDuplicate = "binding_duplicate"
	ProblemBindingOptions   = "binding_options_mismatch"
	ProblemMetaValidation   = "meta_validation"
)

type Problems []Problem

func (p Problems) Error() string {
	parts := make([]string, 0, len(p))
	for _, problem := range p {
		parts = append(parts, fmt.Sprintf("%s/%s.%s:%s", problem.Screen, problem.Block, problem.Property, problem.Code))
	}
	return ErrDocumentInvalid.Error() + ": " + strings.Join(parts, ", ")
}

func (p Problems) Unwrap() error { return ErrDocumentInvalid }

func (p Problems) Err() error {
	if len(p) == 0 {
		return nil
	}
	return p
}

var errorCodes = []struct {
	err  error
	code string
}{
	{ErrForbidden, "forbidden"},
	{ErrFormNotFound, "whatsapp_form_not_found"},
	{ErrFormArchived, "whatsapp_form_archived"},
	{ErrNameRequired, "whatsapp_form_name_required"},
	{ErrNameTooLong, "whatsapp_form_name_too_long"},
	{ErrNameTaken, "whatsapp_form_name_taken"},
	{ErrStaleDraft, "whatsapp_form_stale_draft"},
	{ErrVersionNotFound, "whatsapp_form_version_not_found"},
	{ErrDocumentInvalid, "whatsapp_form_invalid"},
	{ErrNotOwner, "whatsapp_form_not_owner"},
	{ErrProviderUnsupported, "whatsapp_form_provider_unsupported"},
	{ErrNoEligibleAccount, "whatsapp_form_no_account"},
	{ErrNoPublication, "whatsapp_form_not_published"},
	{ErrPublicationNotSendable, "whatsapp_form_not_sendable"},
	{ErrPublicationNotFound, "whatsapp_form_publication_not_found"},
	{ErrOutsideWindow, "whatsapp_form_outside_window"},
	{ErrChannelUnsupported, "whatsapp_form_channel_unsupported"},
	{ErrTokenRequired, "whatsapp_form_token_required"},
	{ErrTokenInvalid, "whatsapp_form_token_invalid"},
	{ErrSendNotFound, "whatsapp_form_send_not_found"},
	{ErrResponseNotFound, "whatsapp_form_response_not_found"},
	{ErrResponseInvalid, "whatsapp_form_response_invalid"},
	{ErrCopyInvalid, "whatsapp_form_copy_invalid"},
	{ErrListQueryInvalid, "whatsapp_form_list_invalid"},
	{ErrFormInUse, "whatsapp_form_in_use"},
	{ErrImportUnsupported, "whatsapp_form_import_unsupported"},
	{ErrStarterUnknown, "whatsapp_form_starter_unknown"},
	{ErrDescriptionTooLong, "whatsapp_form_description_too_long"},
	{ErrMetaRejected, "whatsapp_form_meta_rejected"},
	{ErrWorkspaceRequired, "whatsapp_form_workspace_required"},
}

var inputRefusals = []error{
	ErrNameRequired, ErrNameTooLong, ErrStarterUnknown, ErrDescriptionTooLong, ErrDocumentInvalid, ErrCopyInvalid, ErrListQueryInvalid, ErrWorkspaceRequired,
}

func ErrorCode(err error) string {
	for _, known := range errorCodes {
		if errors.Is(err, known.err) {
			return known.code
		}
	}
	return ""
}

func IsInputRefusal(err error) bool {
	for _, refusal := range inputRefusals {
		if errors.Is(err, refusal) {
			return true
		}
	}
	return false
}
