// Package unofficial_whatsapp_repository persists the unofficial WhatsApp
// channel.
//
// It maps between domain entities and GORM records and holds no rules: whether
// a status transition is legal, whether an instance may send, and how a phone
// number is normalized all live in domain/unofficial_whatsapp, so a second
// caller cannot get a different answer than this one.
package unofficial_whatsapp_repository

import (
	"errors"
	"strings"

	"gorm.io/gorm"

	uw "vozko/domain/unofficial_whatsapp"
	"vozko/infra/crypto/piigorm"
	"vozko/infra/database/schema"
)

// ---------------------------------------------------------------- server

func toServerSchema(s *uw.Server) *schema.UnofficialWhatsAppServer {
	record := &schema.UnofficialWhatsAppServer{
		ID:            s.ID,
		WorkspaceID:   s.WorkspaceID,
		Name:          s.Name,
		Provider:      s.Provider,
		BaseURL:       s.BaseURL,
		Capacity:      s.Capacity,
		InUse:         s.InUse,
		Enabled:       s.Enabled,
		Draining:      s.Draining,
		LastHealthyAt: s.LastHealthyAt,
		LastError:     truncate(s.LastError, 500),
	}
	if s.AdminToken != "" {
		record.AdminToken = piigorm.NewEncrypted(s.AdminToken)
	}
	return record
}

func toServerDomain(record *schema.UnofficialWhatsAppServer) *uw.Server {
	if record == nil {
		return nil
	}
	return &uw.Server{
		ID:            record.ID,
		WorkspaceID:   record.WorkspaceID,
		Name:          record.Name,
		Provider:      record.Provider,
		BaseURL:       record.BaseURL,
		AdminToken:    record.AdminToken.Plain,
		Capacity:      record.Capacity,
		InUse:         record.InUse,
		Enabled:       record.Enabled,
		Draining:      record.Draining,
		LastHealthyAt: record.LastHealthyAt,
		LastError:     record.LastError,
		CreatedAt:     record.CreatedAt,
		UpdatedAt:     record.UpdatedAt,
	}
}

// ---------------------------------------------------------------- instance

func toInstanceSchema(i *uw.Instance) *schema.UnofficialWhatsAppInstance {
	record := &schema.UnofficialWhatsAppInstance{
		ID:           i.ID,
		WorkspaceID:  i.WorkspaceID,
		DepartmentID: i.DepartmentID,
		ServerID:     i.ServerID,
		Provider:     i.Provider,

		ProviderInstanceID: i.ProviderInstanceID,
		ProviderName:       i.ProviderName,
		DeliveryTokenHash:  i.DeliveryTokenHash,
		WebhookSetAt:       i.WebhookSetAt,

		JID:            i.JID,
		LID:            i.LID,
		PhoneNumber:    i.PhoneNumber,
		ProfileName:    i.ProfileName,
		ProfilePicURL:  i.ProfilePicURL,
		IsBusinessAcct: i.IsBusinessAcct,
		Platform:       i.Platform,
		DisplayName:    i.DisplayName,

		Status:       string(i.Status),
		StatusReason: truncate(i.StatusReason, 255),

		ConnectedAt:          i.ConnectedAt,
		LastDisconnectAt:     i.LastDisconnectAt,
		LastDisconnectReason: truncate(i.LastDisconnectWhy, 255),
		LastPolledAt:         i.LastPolledAt,

		RestrictionCanSendNew: i.Restriction.CanSendNewChats,
		RestrictionKey:        truncate(i.Restriction.Key, 64),
		RestrictionMessage:    truncate(i.Restriction.Message, 500),
		RestrictionUntil:      i.Restriction.Until,
		RestrictionUsedQuota:  i.Restriction.UsedQuota,
		RestrictionTotalQuota: i.Restriction.TotalQuota,
		RestrictionCheckedAt:  i.Restriction.CheckedAt,

		DailySendCap:    i.DailySendCap,
		WarmupStartedAt: i.WarmupStartedAt,
		SendDelayMinMS:  i.SendDelayMinMS,
		SendDelayMaxMS:  i.SendDelayMaxMS,
		AutoRejectCalls: i.AutoRejectCalls,

		AgentID:              i.AgentID,
		WorkflowID:           i.WorkflowID,
		PipelineID:           i.PipelineID,
		EnableAgentResponses: i.EnableAgentResponses,
		EnableWorkflow:       i.EnableWorkflow,
		EnableAnalysis:       i.EnableAnalysis,
		EnableAutoStaging:    i.EnableAutoStaging,
		HandleGroups:         i.HandleGroups,
	}
	// Credentials are written only when supplied, so a config-only update
	// cannot blank a live token and silently take the number offline.
	if i.InstanceToken != "" {
		record.InstanceToken = piigorm.NewEncrypted(i.InstanceToken)
	}
	if i.DeliveryToken != "" {
		record.DeliveryToken = piigorm.NewEncrypted(i.DeliveryToken)
	}
	return record
}

func toInstanceDomain(record *schema.UnofficialWhatsAppInstance) *uw.Instance {
	if record == nil {
		return nil
	}
	return &uw.Instance{
		ID:           record.ID,
		WorkspaceID:  record.WorkspaceID,
		DepartmentID: record.DepartmentID,
		ServerID:     record.ServerID,
		Provider:     record.Provider,

		ProviderInstanceID: record.ProviderInstanceID,
		ProviderName:       record.ProviderName,
		InstanceToken:      record.InstanceToken.Plain,
		DeliveryToken:      record.DeliveryToken.Plain,
		DeliveryTokenHash:  record.DeliveryTokenHash,
		WebhookSetAt:       record.WebhookSetAt,

		JID:            record.JID,
		LID:            record.LID,
		PhoneNumber:    record.PhoneNumber,
		ProfileName:    record.ProfileName,
		ProfilePicURL:  record.ProfilePicURL,
		IsBusinessAcct: record.IsBusinessAcct,
		Platform:       record.Platform,
		DisplayName:    record.DisplayName,

		Status:       uw.Status(record.Status),
		StatusReason: record.StatusReason,

		ConnectedAt:       record.ConnectedAt,
		LastDisconnectAt:  record.LastDisconnectAt,
		LastDisconnectWhy: record.LastDisconnectReason,
		LastPolledAt:      record.LastPolledAt,

		Restriction: uw.Restriction{
			CanSendNewChats: record.RestrictionCanSendNew,
			Key:             record.RestrictionKey,
			Message:         record.RestrictionMessage,
			Until:           record.RestrictionUntil,
			UsedQuota:       record.RestrictionUsedQuota,
			TotalQuota:      record.RestrictionTotalQuota,
			CheckedAt:       record.RestrictionCheckedAt,
		},

		DailySendCap:    record.DailySendCap,
		WarmupStartedAt: record.WarmupStartedAt,
		SendDelayMinMS:  record.SendDelayMinMS,
		SendDelayMaxMS:  record.SendDelayMaxMS,
		AutoRejectCalls: record.AutoRejectCalls,

		AgentID:              record.AgentID,
		WorkflowID:           record.WorkflowID,
		PipelineID:           record.PipelineID,
		EnableAgentResponses: record.EnableAgentResponses,
		EnableWorkflow:       record.EnableWorkflow,
		EnableAnalysis:       record.EnableAnalysis,
		EnableAutoStaging:    record.EnableAutoStaging,
		HandleGroups:         record.HandleGroups,

		CreatedAt: record.CreatedAt,
		UpdatedAt: record.UpdatedAt,
	}
}

// ---------------------------------------------------------------- contact

func toContactDomain(record *schema.UnofficialWhatsAppContact) *uw.Contact {
	if record == nil {
		return nil
	}
	return &uw.Contact{
		ID:               record.ID,
		WorkspaceID:      record.WorkspaceID,
		InstanceID:       record.InstanceID,
		JID:              record.JID,
		LID:              record.LID,
		IsGroup:          record.IsGroup,
		PhoneNumber:      record.PhoneNumber,
		LeadID:           record.LeadID,
		Name:             record.Name,
		ContactName:      record.ContactName,
		VerifiedName:     record.VerifiedName,
		PictureURL:       record.PictureURL,
		PictureSourceURL: record.PictureSourceURL,
		IsBusiness:       record.IsBusiness,
		ProfileFetchedAt: record.ProfileFetchedAt,
		Blocked:          record.Blocked,
		BlockedAt:        record.BlockedAt,
		CreatedAt:        record.CreatedAt,
		UpdatedAt:        record.UpdatedAt,
	}
}

// ---------------------------------------------------------------- conversation

func toConversationDomain(record *schema.UnofficialWhatsAppConversation) *uw.Conversation {
	if record == nil {
		return nil
	}
	return &uw.Conversation{
		ID:                    record.ID,
		WorkspaceID:           record.WorkspaceID,
		InstanceID:            record.InstanceID,
		ContactID:             record.ContactID,
		ChatID:                record.ChatID,
		IsGroup:               record.IsGroup,
		ConversationStatus:    record.ConversationStatus,
		CloseSource:           record.CloseSource,
		CloseReason:           record.CloseReason,
		ClosedAt:              record.ClosedAt,
		AutomationEnabled:     record.AutomationEnabled,
		LastMessageAt:         record.LastMessageAt,
		LastCustomerMessageAt: record.LastCustomerMessageAt,
		LastAgentMessageAt:    record.LastAgentMessageAt,
		CreatedAt:             record.CreatedAt,
		UpdatedAt:             record.UpdatedAt,
	}
}

// ---------------------------------------------------------------- group

func toGroupDomain(
	record *schema.UnofficialWhatsAppGroup,
	participants []schema.UnofficialWhatsAppGroupParticipant,
) *uw.Group {
	if record == nil {
		return nil
	}
	g := &uw.Group{
		ID:               record.ID,
		WorkspaceID:      record.WorkspaceID,
		InstanceID:       record.InstanceID,
		JID:              record.JID,
		Subject:          record.Subject,
		Description:      record.Description,
		OwnerJID:         record.OwnerJID,
		Announce:         record.Announce,
		Locked:           record.Locked,
		JoinApproval:     record.JoinApproval,
		Ephemeral:        record.Ephemeral,
		DisappearingSecs: record.DisappearingSecs,
		Community:        record.Community,
		WeAreAdmin:       record.WeAreAdmin,
		WeCanSend:        record.WeCanSend,
		ParticipantCount: record.ParticipantCount,
		GroupCreatedAt:   record.GroupCreatedAt,
		SyncedAt:         record.SyncedAt,
		StaleAt:          record.StaleAt,
		CreatedAt:        record.CreatedAt,
		UpdatedAt:        record.UpdatedAt,
	}
	for i := range participants {
		g.Participants = append(g.Participants, toGroupParticipantDomain(&participants[i]))
	}
	return g
}

func toGroupParticipantDomain(record *schema.UnofficialWhatsAppGroupParticipant) uw.GroupParticipant {
	return uw.GroupParticipant{
		ID:          record.ID,
		GroupID:     record.GroupID,
		JID:         record.JID,
		LID:         record.LID,
		PhoneNumber: record.PhoneNumber,
		DisplayName: record.DisplayName,
		Role:        uw.GroupRole(record.Role),
		ContactID:   record.ContactID,
		CreatedAt:   record.CreatedAt,
		UpdatedAt:   record.UpdatedAt,
	}
}

// ---------------------------------------------------------------- helpers

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// isUniqueViolation recognises a Postgres unique-index conflict without
// depending on the driver's concrete error type.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate key") || strings.Contains(msg, "unique constraint")
}
