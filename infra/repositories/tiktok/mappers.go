package tiktok_repository

import (
	"encoding/json"
	"strings"

	"gorm.io/datatypes"

	ttdomain "vozko/domain/tiktok"
	"vozko/infra/crypto/piigorm"
	"vozko/infra/database/schema"
)

func toAccountSchema(a *ttdomain.Account) *schema.TikTokAccount {
	record := &schema.TikTokAccount{
		ID:                    a.ID,
		WorkspaceID:           a.WorkspaceID,
		DepartmentID:          a.DepartmentID,
		OpenID:                a.OpenID,
		UniqueIdentifier:      a.UniqueIdentifier,
		Username:              strings.TrimSpace(a.Username),
		DisplayName:           strings.TrimSpace(a.DisplayName),
		AvatarKey:             a.AvatarKey,
		HasAdvancedAccess:     a.HasAdvancedAccess,
		IsBusinessAccount:     a.IsBusinessAccount,
		AccessExpiresAt:       a.AccessExpiresAt,
		RefreshExpiresAt:      a.RefreshExpiresAt,
		GrantedScopes:         strings.Join(a.GrantedScopes, ","),
		CanReadVideos:         a.Capabilities.CanReadVideos,
		CanReadComments:       a.Capabilities.CanReadComments,
		CanManageComments:     a.Capabilities.CanManageComments,
		CanReceiveMessages:    a.Capabilities.CanReceiveMessages,
		CanSendMessages:       a.Capabilities.CanSendMessages,
		CanManageInboxProfile: a.Capabilities.CanManageInboxProfile,
		Status:                string(a.Status),
		StatusReason:          a.StatusReason,
		WebhooksHealthy:       a.WebhooksHealthy,
		LastHealthCheckAt:     a.LastHealthCheckAt,
		AgentID:               a.AgentID,
		WorkflowID:            a.WorkflowID,
		PipelineID:            a.PipelineID,
		EnableAgentResponses:  a.EnableAgentResponses,
		EnableWorkflow:        a.EnableWorkflow,
		EnableAnalysis:        a.EnableAnalysis,
		EnableAutoStaging:     a.EnableAutoStaging,
		EnableAutoMemory:      a.EnableAutoMemory,
		AutomationDisclosure:  strings.TrimSpace(a.AutomationDisclosure),
		PurgeAfter:            a.PurgeAfter,
	}
	if a.AccessToken != "" {
		record.AccessToken = piigorm.NewEncrypted(a.AccessToken)
	}
	if a.RefreshToken != "" {
		record.RefreshToken = piigorm.NewEncrypted(a.RefreshToken)
	}
	return record
}

func toAccountDomain(r *schema.TikTokAccount) *ttdomain.Account {
	return &ttdomain.Account{
		ID:                r.ID,
		WorkspaceID:       r.WorkspaceID,
		DepartmentID:      r.DepartmentID,
		OpenID:            r.OpenID,
		UniqueIdentifier:  r.UniqueIdentifier,
		Username:          r.Username,
		DisplayName:       r.DisplayName,
		AvatarKey:         r.AvatarKey,
		HasAdvancedAccess: r.HasAdvancedAccess,
		IsBusinessAccount: r.IsBusinessAccount,
		AccessToken:       r.AccessToken.Plain,
		RefreshToken:      r.RefreshToken.Plain,
		AccessExpiresAt:   r.AccessExpiresAt,
		RefreshExpiresAt:  r.RefreshExpiresAt,
		GrantedScopes:     ttdomain.ParseScopes(r.GrantedScopes),
		Capabilities: ttdomain.AccountCapabilities{
			CanReadVideos:         r.CanReadVideos,
			CanReadComments:       r.CanReadComments,
			CanManageComments:     r.CanManageComments,
			CanReceiveMessages:    r.CanReceiveMessages,
			CanSendMessages:       r.CanSendMessages,
			CanManageInboxProfile: r.CanManageInboxProfile,
		},
		Status:               ttdomain.Status(r.Status),
		StatusReason:         r.StatusReason,
		WebhooksHealthy:      r.WebhooksHealthy,
		LastHealthCheckAt:    r.LastHealthCheckAt,
		AgentID:              r.AgentID,
		WorkflowID:           r.WorkflowID,
		PipelineID:           r.PipelineID,
		EnableAgentResponses: r.EnableAgentResponses,
		EnableWorkflow:       r.EnableWorkflow,
		EnableAnalysis:       r.EnableAnalysis,
		EnableAutoStaging:    r.EnableAutoStaging,
		EnableAutoMemory:     r.EnableAutoMemory,
		AutomationDisclosure: r.AutomationDisclosure,
		PurgeAfter:           r.PurgeAfter,
		CreatedAt:            r.CreatedAt,
		UpdatedAt:            r.UpdatedAt,
	}
}

func toAccountDomains(records []schema.TikTokAccount) []*ttdomain.Account {
	out := make([]*ttdomain.Account, len(records))
	for i := range records {
		out[i] = toAccountDomain(&records[i])
	}
	return out
}

func toContactDomain(r *schema.TikTokContact) *ttdomain.Contact {
	return &ttdomain.Contact{
		ID:                 r.ID,
		WorkspaceID:        r.WorkspaceID,
		AccountID:          r.AccountID,
		UniqueIdentifier:   r.UniqueIdentifier,
		Username:           r.Username,
		DisplayName:        r.DisplayName,
		AvatarKey:          r.AvatarKey,
		IsFollower:         r.IsFollower,
		ProfileRefreshedAt: r.ProfileRefreshedAt,
		LeadID:             r.LeadID,
		CreatedAt:          r.CreatedAt,
		UpdatedAt:          r.UpdatedAt,
	}
}

func toConversationDomain(r *schema.TikTokConversation) *ttdomain.Conversation {
	c := &ttdomain.Conversation{
		ID:                    r.ID,
		WorkspaceID:           r.WorkspaceID,
		AccountID:             r.AccountID,
		ContactID:             r.ContactID,
		ConversationID:        r.ConversationID,
		ConversationType:      ttdomain.ParseConversationType(r.ConversationType),
		OutboundSinceInbound:  r.OutboundSinceInbound,
		BudgetClosedAt:        r.BudgetClosedAt,
		ImageSendAllowed:      r.ImageSendAllowed,
		ImageSendCheckedAt:    r.ImageSendCheckedAt,
		ConversationStatus:    r.ConversationStatus,
		AutomationEnabled:     r.AutomationEnabled,
		ClosedAt:              r.ClosedAt,
		LastMessageAt:         r.LastMessageAt,
		LastCustomerMessageAt: r.LastCustomerMessageAt,
		CreatedAt:             r.CreatedAt,
		UpdatedAt:             r.UpdatedAt,
	}
	if len(r.Referral) > 0 && string(r.Referral) != "null" {
		var ref ttdomain.Referral
		if err := json.Unmarshal(r.Referral, &ref); err == nil {
			c.Referral = &ref
		}
	}
	return c
}

func referralJSON(ref ttdomain.Referral) (datatypes.JSON, error) {
	raw, err := json.Marshal(ref)
	if err != nil {
		return nil, err
	}
	return datatypes.JSON(raw), nil
}

func toVideoDomain(r *schema.TikTokVideo) *ttdomain.Video {
	return &ttdomain.Video{
		ID:           r.ID,
		WorkspaceID:  r.WorkspaceID,
		AccountID:    r.AccountID,
		ItemID:       r.ItemID,
		MediaType:    ttdomain.MediaType(r.MediaType),
		Caption:      r.Caption,
		ShareURL:     r.ShareURL,
		EmbedURL:     r.EmbedURL,
		ThumbnailKey: r.ThumbnailKey,
		IsAd:         r.IsAd,
		Likes:        r.Likes,
		Comments:     r.Comments,
		Shares:       r.Shares,
		Views:        r.Views,
		PostedAt:     r.PostedAt,
		SyncedAt:     r.SyncedAt,
		CreatedAt:    r.CreatedAt,
		UpdatedAt:    r.UpdatedAt,
	}
}

func toCommentDomain(r *schema.TikTokComment) *ttdomain.Comment {
	return &ttdomain.Comment{
		ID:                     r.ID,
		WorkspaceID:            r.WorkspaceID,
		AccountID:              r.AccountID,
		VideoID:                r.VideoID,
		CommentID:              r.CommentID,
		ParentCommentID:        r.ParentCommentID,
		Kind:                   ttdomain.CommentKind(r.Kind),
		ContactID:              r.ContactID,
		AuthorUniqueIdentifier: r.AuthorUniqueIdentifier,
		AuthorUsername:         r.AuthorUsername,
		AuthorDisplayName:      r.AuthorDisplayName,
		Text:                   r.Text,
		ImageURL:               r.ImageURL,
		Visibility:             ttdomain.CommentVisibility(r.Visibility),
		IsOurs:                 r.IsOurs,
		LikedByUs:              r.LikedByUs,
		Likes:                  r.Likes,
		Replies:                r.Replies,
		CommentedAt:            r.CommentedAt,
		LastEventAt:            r.LastEventAt,
		Lookup:                 ttdomain.LookupState(r.Lookup),
		LookupAttempts:         r.LookupAttempts,
		NextLookupAt:           r.NextLookupAt,
		RulesDue:               r.RulesDue,
		CreatedAt:              r.CreatedAt,
		UpdatedAt:              r.UpdatedAt,
	}
}
