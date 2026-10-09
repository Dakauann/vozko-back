package leadaction

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"vozko/domain/calls/calllist"
	"vozko/domain/workspace"
)

type Action string

const (
	ActionClassify     Action = "classify"
	ActionAssignOwner  Action = "assign_owner"
	ActionBlock        Action = "block"
	ActionExport       Action = "export"
	ActionMetaAudience Action = "meta_audience"
	ActionCallList     Action = "call_list"
)

const (
	CapabilityBulkEdit        workspace.CapabilityKey = "leads.bulk_edit"
	CapabilityAssign          workspace.CapabilityKey = "leads.assign"
	CapabilityBlock           workspace.CapabilityKey = "leads.block"
	CapabilityExport          workspace.CapabilityKey = "leads.export"
	CapabilityMetaAudience    workspace.CapabilityKey = "leads.meta_audience"
	CapabilityReadSensitive   workspace.CapabilityKey = "leads.read_sensitive"
	CapabilityReadAddresses   workspace.CapabilityKey = "leads.read_addresses"
	CapabilityManageCallLists                         = calllist.CapabilityManage
)

const ExportFormatCSV = "csv"

func Actions() []Action {
	return []Action{ActionClassify, ActionAssignOwner, ActionBlock, ActionExport, ActionMetaAudience, ActionCallList, ActionSendTemplate, ActionSendUnofficial}
}

func (a Action) Known() bool {
	for _, known := range Actions() {
		if a == known {
			return true
		}
	}
	return false
}

func (a Action) Runs() bool {
	return a == ActionClassify || a == ActionAssignOwner || a == ActionBlock
}

func (a Action) Requirements(p Params, sensitiveField bool) []workspace.CapabilityKey {
	switch a {
	case ActionClassify:
		if sensitiveField {
			return []workspace.CapabilityKey{CapabilityBulkEdit, CapabilityReadSensitive}
		}
		return []workspace.CapabilityKey{CapabilityBulkEdit}
	case ActionAssignOwner:
		return []workspace.CapabilityKey{CapabilityBulkEdit, CapabilityAssign}
	case ActionBlock:
		return []workspace.CapabilityKey{CapabilityBulkEdit, CapabilityBlock}
	case ActionExport:
		keys := []workspace.CapabilityKey{CapabilityExport}
		if p.Addresses {
			keys = append(keys, CapabilityReadAddresses)
		}
		if p.Sensitive {
			keys = append(keys, CapabilityReadSensitive)
		}
		return keys
	case ActionMetaAudience:
		return []workspace.CapabilityKey{CapabilityMetaAudience}
	case ActionCallList:
		return []workspace.CapabilityKey{CapabilityManageCallLists}
	case ActionSendTemplate:
		return []workspace.CapabilityKey{CapabilitySendTemplate}
	case ActionSendUnofficial:
		return []workspace.CapabilityKey{CapabilitySendUnofficial}
	}
	return nil
}

func PermissionsOf(keys []workspace.CapabilityKey) ([]workspace.PermissionEntry, error) {
	entries, err := workspace.CapabilitiesRequire(keys)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrRequirementUnknown, err)
	}
	return entries, nil
}

func ExportRequirements(addresses, sensitive bool) ([]workspace.PermissionEntry, error) {
	return PermissionsOf(ActionExport.Requirements(Params{Addresses: addresses, Sensitive: sensitive}, false))
}

type Params struct {
	Key             string          `json:"key,omitempty"`
	Value           json.RawMessage `json:"value,omitempty" swaggertype:"object"`
	ValueRedacted   bool            `json:"valueRedacted,omitempty"`
	OwnerID         *string         `json:"ownerId,omitempty"`
	Blocked         *bool           `json:"blocked,omitempty"`
	BusinessPhoneID string          `json:"businessPhoneId,omitempty"`
	Format          string          `json:"format,omitempty"`
	Addresses       bool            `json:"addresses,omitempty"`
	Sensitive       bool            `json:"sensitive,omitempty"`
	AdAccountID     string          `json:"adAccountId,omitempty"`
	Name            string          `json:"name,omitempty"`
	Description     string          `json:"description,omitempty"`
	CallList        *CallListParams `json:"callList,omitempty"`
	Send            *SendParams     `json:"send,omitempty" swaggerignore:"true"`
}

type CallListParams struct {
	Name        string   `json:"name"`
	AssigneeIDs []string `json:"assigneeIds"`
	PhoneSource string   `json:"phoneSource"`
	PhoneLabel  string   `json:"phoneLabel,omitempty"`
}

func (c *CallListParams) normalized() *CallListParams {
	if c == nil {
		return nil
	}
	out := *c
	out.Name = strings.TrimSpace(out.Name)
	out.PhoneSource = strings.TrimSpace(out.PhoneSource)
	out.PhoneLabel = strings.TrimSpace(out.PhoneLabel)
	out.AssigneeIDs = append([]string{}, c.AssigneeIDs...)
	return &out
}

func (p Params) Normalized() Params {
	p.Key = strings.TrimSpace(p.Key)
	if p.OwnerID != nil {
		owner := strings.TrimSpace(*p.OwnerID)
		p.OwnerID = &owner
	}
	p.BusinessPhoneID = strings.TrimSpace(p.BusinessPhoneID)
	p.Format = strings.ToLower(strings.TrimSpace(p.Format))
	p.AdAccountID = strings.TrimSpace(p.AdAccountID)
	p.Name = strings.TrimSpace(p.Name)
	p.Description = strings.TrimSpace(p.Description)
	p.CallList = p.CallList.normalized()
	if p.Send != nil {
		send := p.Send.normalized()
		p.Send = &send
	}
	return p
}

func (p Params) ClearsValue() bool {
	return bytes.Equal(bytes.TrimSpace(p.Value), []byte("null"))
}

func (p Params) Validate(a Action) error {
	p = p.Normalized()
	if a.Sends() {
		return p.validateSend(a)
	}
	if p.Send != nil && a.Known() {
		return ErrParamsAmbiguous
	}
	if a == ActionCallList {
		return p.validateCallList()
	}
	if p.CallList != nil && a.Known() {
		return ErrParamsAmbiguous
	}
	switch a {
	case ActionClassify:
		if p.OwnerID != nil || p.Blocked != nil || p.BusinessPhoneID != "" || p.exportFields() || p.audienceFields() {
			return ErrParamsAmbiguous
		}
		if p.Key == "" {
			return ErrKeyRequired
		}
		if len(bytes.TrimSpace(p.Value)) == 0 {
			return ErrValueRequired
		}
	case ActionAssignOwner:
		if p.Key != "" || len(p.Value) > 0 || p.Blocked != nil || p.BusinessPhoneID != "" || p.exportFields() || p.audienceFields() {
			return ErrParamsAmbiguous
		}
		if p.OwnerID == nil {
			return ErrOwnerRequired
		}
	case ActionBlock:
		if p.Key != "" || len(p.Value) > 0 || p.OwnerID != nil || p.exportFields() || p.audienceFields() {
			return ErrParamsAmbiguous
		}
		if p.Blocked == nil {
			return ErrBlockedRequired
		}
	case ActionExport:
		if p.Key != "" || len(p.Value) > 0 || p.OwnerID != nil || p.Blocked != nil || p.BusinessPhoneID != "" || p.audienceFields() {
			return ErrParamsAmbiguous
		}
		if p.Format != "" && p.Format != ExportFormatCSV {
			return ErrFormatUnsupported
		}
	case ActionMetaAudience:
		if p.Key != "" || len(p.Value) > 0 || p.OwnerID != nil || p.Blocked != nil || p.BusinessPhoneID != "" || p.exportFields() {
			return ErrParamsAmbiguous
		}
		if p.AdAccountID == "" {
			return ErrAdAccountRequired
		}
		if p.Name == "" {
			return ErrNameRequired
		}
	default:
		return fmt.Errorf("%w: %q", ErrUnknownAction, a)
	}
	return nil
}

func (p Params) validateCallList() error {
	if p.Key != "" || len(p.Value) > 0 || p.OwnerID != nil || p.Blocked != nil || p.BusinessPhoneID != "" || p.exportFields() || p.audienceFields() {
		return ErrParamsAmbiguous
	}
	if p.CallList == nil {
		return ErrCallListRequired
	}
	return nil
}

func (p Params) exportFields() bool {
	return p.Format != "" || p.Addresses || p.Sensitive
}

func (p Params) audienceFields() bool {
	return p.AdAccountID != "" || p.Name != "" || p.Description != ""
}

func (p Params) Redacted(sensitiveField bool) Params {
	if !sensitiveField || len(p.Value) == 0 {
		return p
	}
	p.Value, p.ValueRedacted = nil, true
	return p
}
