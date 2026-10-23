package carriermessaging_repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"vozko/domain/carriermessaging"
	"vozko/domain/conversation"
	"vozko/domain/rcs"
	"vozko/infra/database/schema"
)

var errUnknownStoredValue = errors.New("rcs: unknown stored value")

type validated interface {
	~string
	Valid() bool
}

func valid[T validated](field string, value T) error {
	if value.Valid() {
		return nil
	}
	return fmt.Errorf("%w: %s %q", errUnknownStoredValue, field, value)
}

func validOrEmpty[T validated](field string, value T) error {
	if value == "" {
		return nil
	}
	return valid(field, value)
}

func filtered[T any](set []T, keep func(T) bool) []T {
	var out []T
	for _, v := range set {
		if keep(v) {
			out = append(out, v)
		}
	}
	return out
}

func newID(id string) string {
	if id != "" {
		return id
	}
	return uuid.NewString()
}

func optionalID(id string) *string {
	if id = strings.TrimSpace(id); id == "" {
		return nil
	}
	return &id
}

func idOrEmpty(id *string) string {
	if id == nil {
		return ""
	}
	return *id
}

func encodeJSON(field string, v any) (datatypes.JSON, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("rcs: encoding %s: %w", field, err)
	}
	return datatypes.JSON(raw), nil
}

func decodeJSON(field string, raw datatypes.JSON, into any) error {
	if err := json.Unmarshal(raw, into); err != nil {
		return fmt.Errorf("rcs: decoding stored %s: %w", field, err)
	}
	return nil
}

func rowExists(ctx context.Context, db *gorm.DB, model any, id string) (bool, error) {
	var n int64
	if err := db.WithContext(ctx).Model(model).Where("id = ?", id).Count(&n).Error; err != nil {
		return false, err
	}
	return n > 0, nil
}

func changedOnce(ctx context.Context, db *gorm.DB, result *gorm.DB, model any, id string, notFound error) (bool, error) {
	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected > 0 {
		return true, nil
	}
	exists, err := rowExists(ctx, db, model, id)
	if err != nil {
		return false, err
	}
	if !exists {
		return false, notFound
	}
	return false, nil
}

func toAutomationColumns(a conversation.ChannelAutomation) schema.ChannelAutomationColumns {
	return schema.ChannelAutomationColumns{
		AgentID:              a.AgentID,
		WorkflowID:           a.WorkflowID,
		EnableAgentResponses: a.EnableAgentResponses,
		EnableWorkflow:       a.EnableWorkflow,
		EnableAnalysis:       a.EnableAnalysis,
		EnableAutoStaging:    a.EnableAutoStaging,
		EnableAutoMemory:     a.EnableAutoMemory,
		AutomationDisclosure: a.Disclosure,
	}
}

func toAutomation(c schema.ChannelAutomationColumns) conversation.ChannelAutomation {
	return conversation.ChannelAutomation{
		AgentID:              c.AgentID,
		WorkflowID:           c.WorkflowID,
		EnableAgentResponses: c.EnableAgentResponses,
		EnableWorkflow:       c.EnableWorkflow,
		EnableAnalysis:       c.EnableAnalysis,
		EnableAutoStaging:    c.EnableAutoStaging,
		EnableAutoMemory:     c.EnableAutoMemory,
		Disclosure:           c.AutomationDisclosure,
	}
}

func toSenderSchema(s *rcs.Sender) (*schema.RCSSender, error) {
	if err := validSender(s.Status, s.BillingCategory, s.UseCase, s.Sector, s.PausedBy, s.PauseReason); err != nil {
		return nil, err
	}
	return &schema.RCSSender{
		ID:                       s.ID,
		WorkspaceID:              s.WorkspaceID,
		BrandName:                s.BrandName,
		Description:              s.Description,
		LogoURL:                  s.LogoURL,
		BannerURL:                s.BannerURL,
		Color:                    s.Color,
		UseCase:                  string(s.UseCase),
		Sector:                   string(s.Sector),
		GamblingAuthorizationURL: s.GamblingAuthorizationURL,
		ContactName:              s.ContactName,
		ContactEmail:             s.ContactEmail,
		ContactPhone:             s.ContactPhone,
		WebsiteURL:               s.WebsiteURL,
		PrivacyURL:               s.PrivacyURL,
		TermsURL:                 s.TermsURL,
		CNPJ:                     s.CNPJ,
		BillingCategory:          string(s.BillingCategory),
		Status:                   string(s.Status),
		StatusReason:             s.StatusReason,
		PausedBy:                 string(s.PausedBy),
		PauseReason:              string(s.PauseReason),
		Provider:                 s.Provider,
		ProviderProfileID:        s.ProviderProfileID,
		ProviderRequestID:        s.ProviderRequestID,
		ChannelAutomationColumns: toAutomationColumns(s.Automation),
		ChannelContainerColumns:  schema.ChannelContainerColumns{DepartmentID: s.DepartmentID, PipelineID: s.PipelineID},
		SubmittedAt:              s.SubmittedAt,
		LaunchedAt:               s.LaunchedAt,
		PausedAt:                 s.PausedAt,
		CreatedAt:                s.CreatedAt,
		UpdatedAt:                s.UpdatedAt,
	}, nil
}

func validSender(status rcs.SenderStatus, billing rcs.BillingCategory, useCase rcs.UseCase, sector rcs.Sector, pausedBy rcs.PausedBy, pauseReason rcs.PauseReason) error {
	return errors.Join(
		valid("sender status", status),
		validOrEmpty("billing category", billing),
		validOrEmpty("use case", useCase),
		validOrEmpty("sector", sector),
		validOrEmpty("paused by", pausedBy),
		validOrEmpty("pause reason", pauseReason),
	)
}

func toSender(r *schema.RCSSender) (*rcs.Sender, error) {
	s := &rcs.Sender{
		ID:                       r.ID,
		WorkspaceID:              r.WorkspaceID,
		BrandName:                r.BrandName,
		Description:              r.Description,
		LogoURL:                  r.LogoURL,
		BannerURL:                r.BannerURL,
		Color:                    r.Color,
		UseCase:                  rcs.UseCase(r.UseCase),
		Sector:                   rcs.Sector(r.Sector),
		GamblingAuthorizationURL: r.GamblingAuthorizationURL,
		ContactName:              r.ContactName,
		ContactEmail:             r.ContactEmail,
		ContactPhone:             r.ContactPhone,
		WebsiteURL:               r.WebsiteURL,
		PrivacyURL:               r.PrivacyURL,
		TermsURL:                 r.TermsURL,
		CNPJ:                     r.CNPJ,
		BillingCategory:          rcs.BillingCategory(r.BillingCategory),
		Status:                   rcs.SenderStatus(r.Status),
		StatusReason:             r.StatusReason,
		PausedBy:                 rcs.PausedBy(r.PausedBy),
		PauseReason:              rcs.PauseReason(r.PauseReason),
		Provider:                 r.Provider,
		ProviderProfileID:        r.ProviderProfileID,
		ProviderRequestID:        r.ProviderRequestID,
		Automation:               toAutomation(r.ChannelAutomationColumns),
		DepartmentID:             r.DepartmentID,
		PipelineID:               r.PipelineID,
		SubmittedAt:              r.SubmittedAt,
		LaunchedAt:               r.LaunchedAt,
		PausedAt:                 r.PausedAt,
		CreatedAt:                r.CreatedAt,
		UpdatedAt:                r.UpdatedAt,
	}
	if err := validSender(s.Status, s.BillingCategory, s.UseCase, s.Sector, s.PausedBy, s.PauseReason); err != nil {
		return nil, fmt.Errorf("rcs sender %s: %w", r.ID, err)
	}
	return s, nil
}

func toSenders(records []schema.RCSSender) ([]*rcs.Sender, error) {
	out := make([]*rcs.Sender, 0, len(records))
	for i := range records {
		s, err := toSender(&records[i])
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

func validMessage(m *carriermessaging.Message) error {
	class := validOrEmpty("billing class", m.Class)
	if m.Step == carriermessaging.StepRCS {
		class = valid("billing class", m.Class)
	}
	return errors.Join(
		valid("channel", m.Channel),
		valid("message status", m.Status),
		valid("message step", m.Step),
		valid("run kind", m.RunKind),
		class,
	)
}

func toMessageSchema(m *carriermessaging.Message) (*schema.CarrierMessage, error) {
	if err := validMessage(m); err != nil {
		return nil, err
	}
	content, err := encodeJSON("content", m.Content)
	if err != nil {
		return nil, err
	}
	fallback, err := encodeJSON("fallback", m.Fallback)
	if err != nil {
		return nil, err
	}
	return &schema.CarrierMessage{
		ID:                m.ID,
		WorkspaceID:       m.WorkspaceID,
		Channel:           string(m.Channel),
		Reference:         m.Reference,
		SenderID:          m.SenderID,
		Phone:             m.Phone,
		LeadID:            optionalID(m.LeadID),
		ConversationID:    optionalID(m.ConversationID),
		RunKind:           string(m.RunKind),
		Marketing:         m.Marketing,
		RunID:             m.RunID,
		Variant:           m.Variant,
		Content:           content,
		Text:              m.Text,
		Segments:          m.Segments,
		Class:             string(m.Class),
		Step:              string(m.Step),
		Status:            string(m.Status),
		ErrorCode:         m.ErrorCode,
		ParentMessageID:   optionalID(m.ParentMessageID),
		FallbackMessageID: optionalID(m.FallbackMessageID),
		Fallback:          fallback,
		ChargeReference:   m.ChargeReference,
		AcceptedAt:        m.AcceptedAt,
		DeliveredAt:       m.DeliveredAt,
		ReadAt:            m.ReadAt,
		FailedAt:          m.FailedAt,
		ExpiresAt:         m.ExpiresAt,
		CreatedAt:         m.CreatedAt,
	}, nil
}

func toMessage(r *schema.CarrierMessage) (*carriermessaging.Message, error) {
	m := &carriermessaging.Message{
		ID:                r.ID,
		WorkspaceID:       r.WorkspaceID,
		Channel:           carriermessaging.Channel(r.Channel),
		Reference:         r.Reference,
		SenderID:          r.SenderID,
		Phone:             r.Phone,
		LeadID:            idOrEmpty(r.LeadID),
		ConversationID:    idOrEmpty(r.ConversationID),
		RunKind:           carriermessaging.RunKind(r.RunKind),
		Marketing:         r.Marketing,
		RunID:             r.RunID,
		Variant:           r.Variant,
		Text:              r.Text,
		Segments:          r.Segments,
		Class:             rcs.BillingClass(r.Class),
		Step:              carriermessaging.Step(r.Step),
		Status:            carriermessaging.Status(r.Status),
		ErrorCode:         r.ErrorCode,
		ParentMessageID:   idOrEmpty(r.ParentMessageID),
		FallbackMessageID: idOrEmpty(r.FallbackMessageID),
		ChargeReference:   r.ChargeReference,
		AcceptedAt:        r.AcceptedAt,
		DeliveredAt:       r.DeliveredAt,
		ReadAt:            r.ReadAt,
		FailedAt:          r.FailedAt,
		ExpiresAt:         r.ExpiresAt,
		CreatedAt:         r.CreatedAt,
	}
	if err := validMessage(m); err != nil {
		return nil, fmt.Errorf("rcs message %s: %w", r.ID, err)
	}
	if err := decodeJSON("content", r.Content, &m.Content); err != nil {
		return nil, err
	}
	if err := decodeJSON("fallback", r.Fallback, &m.Fallback); err != nil {
		return nil, err
	}
	return m, nil
}

func toMessages(records []schema.CarrierMessage) ([]*carriermessaging.Message, error) {
	out := make([]*carriermessaging.Message, 0, len(records))
	for i := range records {
		m, err := toMessage(&records[i])
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

func toContact(r *schema.CarrierContact) *carriermessaging.Contact {
	return &carriermessaging.Contact{
		ID:          r.ID,
		WorkspaceID: r.WorkspaceID,
		Phone:       r.Phone,
		DisplayName: r.DisplayName,
		LeadID:      r.LeadID,
		FloodingAt:  r.FloodingAt,
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
	}
}

func toConversation(r *schema.CarrierConversation, channel carriermessaging.Channel) *carriermessaging.Conversation {
	return &carriermessaging.Conversation{
		ID:                    r.ID,
		WorkspaceID:           r.WorkspaceID,
		Channel:               channel,
		SenderID:              r.SenderID,
		ContactID:             r.ContactID,
		ConversationStatus:    r.ConversationStatus,
		AutomationEnabled:     r.AutomationEnabled,
		LastMessageAt:         r.LastMessageAt,
		LastCustomerMessageAt: r.LastCustomerMessageAt,
		LastAgentMessageAt:    r.LastAgentMessageAt,
		ConversationOpenedAt:  r.ConversationOpenedAt,
		CreatedAt:             r.CreatedAt,
		UpdatedAt:             r.UpdatedAt,
	}
}
