package emailsending_repository

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"

	"vozko/domain/emailaddress"
	es "vozko/domain/emailsending"
	"vozko/infra/database/schema"
)

func newID(id string) string {
	if id != "" {
		return id
	}
	return uuid.NewString()
}

func optionalTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	v := t.UTC()
	return &v
}

func timeOrZero(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}

func toAccountSchema(a *es.SenderAccount) *schema.EmailSenderAccount {
	return &schema.EmailSenderAccount{
		ID:                 a.ID,
		WorkspaceID:        a.WorkspaceID,
		Provider:           a.Provider,
		TenantName:         a.TenantName,
		TenantRef:          a.TenantRef,
		Mode:               string(a.Mode),
		Status:             string(a.Status),
		StatusReason:       a.StatusReason,
		Slug:               a.Slug,
		ProbationStartedAt: optionalTime(a.ProbationStartedAt),
		PromotedAt:         a.PromotedAt,
		UnusedWarnedAt:     a.UnusedWarnedAt,
		CreatedAt:          a.CreatedAt,
		UpdatedAt:          a.UpdatedAt,
	}
}

func toAccount(r schema.EmailSenderAccount) es.SenderAccount {
	return es.SenderAccount{
		ID:                 r.ID,
		WorkspaceID:        r.WorkspaceID,
		Provider:           r.Provider,
		TenantName:         r.TenantName,
		TenantRef:          r.TenantRef,
		Mode:               es.Mode(r.Mode),
		Status:             es.AccountStatus(r.Status),
		StatusReason:       r.StatusReason,
		Slug:               r.Slug,
		ProbationStartedAt: timeOrZero(r.ProbationStartedAt),
		PromotedAt:         r.PromotedAt,
		UnusedWarnedAt:     r.UnusedWarnedAt,
		CreatedAt:          r.CreatedAt,
		UpdatedAt:          r.UpdatedAt,
	}
}

func toDomainSchema(d *es.SendingDomain) (*schema.EmailSendingDomain, error) {
	records := d.Records
	if records == nil {
		records = []es.DNSRecord{}
	}
	raw, err := json.Marshal(records)
	if err != nil {
		return nil, err
	}
	out := &schema.EmailSendingDomain{
		ID:             d.ID,
		WorkspaceID:    d.WorkspaceID,
		AccountID:      d.AccountID,
		Domain:         d.Domain,
		Kind:           string(d.Kind),
		Status:         string(d.Status),
		ConnectMethod:  string(d.ConnectMethod),
		MailFromDomain: d.MailFromDomain,
		ReplyDomain:    d.ReplyDomain,
		Records:        datatypes.JSON(raw),
		FailureReason:  d.FailureReason,
		SetupFailure:   string(d.SetupFailure),
		ReleaseState:   string(d.ReleaseState),
		VerifiedAt:     d.VerifiedAt,
		LastCheckedAt:  d.LastCheckedAt,
		VerifyingSince: d.VerifyingSince,
		CreatedAt:      d.CreatedAt,
		UpdatedAt:      d.UpdatedAt,
	}
	return out, nil
}

func toDomain(r schema.EmailSendingDomain) (es.SendingDomain, error) {
	if !es.ConnectMethod(r.ConnectMethod).Valid() {
		return es.SendingDomain{}, fmt.Errorf("%w: %q", es.ErrUnknownConnectMethod, r.ConnectMethod)
	}
	if !es.SetupFailure(r.SetupFailure).Valid() {
		return es.SendingDomain{}, fmt.Errorf("%w: %q", es.ErrUnknownSetupFailure, r.SetupFailure)
	}
	if !es.ReleaseState(r.ReleaseState).Valid() {
		return es.SendingDomain{}, fmt.Errorf("%w: %q", es.ErrUnknownReleaseState, r.ReleaseState)
	}
	var records []es.DNSRecord
	if len(r.Records) > 0 {
		if err := json.Unmarshal(r.Records, &records); err != nil {
			return es.SendingDomain{}, err
		}
	}
	return es.SendingDomain{
		ID:              r.ID,
		WorkspaceID:     r.WorkspaceID,
		AccountID:       r.AccountID,
		Domain:          r.Domain,
		Kind:            es.DomainKind(r.Kind),
		Status:          es.DomainStatus(r.Status),
		ConnectMethod:   es.ConnectMethod(r.ConnectMethod),
		MailFromDomain:  r.MailFromDomain,
		ReplyDomain:     r.ReplyDomain,
		Records:         records,
		FailureReason:   r.FailureReason,
		SetupFailure:    es.SetupFailure(r.SetupFailure),
		VerifiedAt:      r.VerifiedAt,
		LastCheckedAt:   r.LastCheckedAt,
		VerifyingSince:  r.VerifyingSince,
		Retired:         r.RetiredAt != nil,
		ReleaseState:    es.ReleaseState(r.ReleaseState),
		RepliesPausedAt: r.RepliesPausedAt,
		CreatedAt:       r.CreatedAt,
		UpdatedAt:       r.UpdatedAt,
	}, nil
}

func toDomains(rows []schema.EmailSendingDomain) ([]es.SendingDomain, error) {
	out := make([]es.SendingDomain, 0, len(rows))
	for _, r := range rows {
		d, err := toDomain(r)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

func toSenderSchema(s *es.Sender) *schema.EmailSender {
	return &schema.EmailSender{
		ID:             s.ID,
		WorkspaceID:    s.WorkspaceID,
		DomainID:       s.DomainID,
		LocalPart:      s.LocalPart,
		DisplayName:    s.DisplayName,
		ReplyForwardTo: string(s.ReplyForwardTo),
		IsDefault:      s.IsDefault,
		CreatedAt:      s.CreatedAt,
		UpdatedAt:      s.UpdatedAt,
	}
}

func toSender(r schema.EmailSender) es.Sender {
	return es.Sender{
		ID:             r.ID,
		WorkspaceID:    r.WorkspaceID,
		DomainID:       r.DomainID,
		LocalPart:      r.LocalPart,
		DisplayName:    r.DisplayName,
		ReplyForwardTo: emailaddress.Address(r.ReplyForwardTo),
		IsDefault:      r.IsDefault,
		CreatedAt:      r.CreatedAt,
		UpdatedAt:      r.UpdatedAt,
	}
}

func toMessageSchema(m *es.Message) *schema.EmailMessage {
	return &schema.EmailMessage{
		ID:                m.ID,
		WorkspaceID:       m.WorkspaceID,
		ProviderMessageID: schema.OptionalText(m.ProviderMessageID),
		RunKind:           string(m.RunKind),
		RunID:             m.RunID,
		RecipientID:       m.RecipientID,
		DomainID:          m.DomainID,
		Address:           string(m.Address),
		Status:            string(m.Status),
		SentAt:            optionalTime(m.SentAt),
		DeliveredAt:       m.DeliveredAt,
		FirstOpenAt:       m.FirstOpenAt,
		FirstClickAt:      m.FirstClickAt,
		BouncedAt:         m.BouncedAt,
		BounceKind:        string(m.BounceKind),
		ComplainedAt:      m.ComplainedAt,
		UnsubscribedAt:    m.UnsubscribedAt,
		Opens:             m.Opens,
		BotOpens:          m.BotOpens,
		Clicks:            m.Clicks,
	}
}

func toMessage(r schema.EmailMessage) es.Message {
	return es.Message{
		ID:                r.ID,
		WorkspaceID:       r.WorkspaceID,
		ProviderMessageID: string(r.ProviderMessageID),
		RunKind:           es.RunKind(r.RunKind),
		RunID:             r.RunID,
		RecipientID:       r.RecipientID,
		DomainID:          r.DomainID,
		Address:           emailaddress.Address(r.Address),
		Status:            es.MessageStatus(r.Status),
		SentAt:            timeOrZero(r.SentAt),
		DeliveredAt:       r.DeliveredAt,
		FirstOpenAt:       r.FirstOpenAt,
		FirstClickAt:      r.FirstClickAt,
		BouncedAt:         r.BouncedAt,
		BounceKind:        es.BounceKind(r.BounceKind),
		ComplainedAt:      r.ComplainedAt,
		UnsubscribedAt:    r.UnsubscribedAt,
		Opens:             r.Opens,
		BotOpens:          r.BotOpens,
		Clicks:            r.Clicks,
	}
}

func messageUpdates(m *es.Message) map[string]any {
	return map[string]any{
		"provider_message_id": schema.OptionalText(m.ProviderMessageID),
		"status":              string(m.Status),
		"sent_at":             optionalTime(m.SentAt),
		"delivered_at":        m.DeliveredAt,
		"first_open_at":       m.FirstOpenAt,
		"first_click_at":      m.FirstClickAt,
		"bounced_at":          m.BouncedAt,
		"bounce_kind":         string(m.BounceKind),
		"complained_at":       m.ComplainedAt,
		"unsubscribed_at":     m.UnsubscribedAt,
		"opens":               m.Opens,
		"bot_opens":           m.BotOpens,
		"clicks":              m.Clicks,
	}
}
