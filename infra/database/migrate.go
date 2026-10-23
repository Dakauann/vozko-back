package database

import (
	"vozko/infra/database/schema"

	"gorm.io/gorm"
)

const (
	migrationLockID      = 123456789
	migrationLockTimeout = "15s"
)

func prepareMigration(tx *gorm.DB) error {
	if err := serializeMigration(tx, migrationLockTimeout); err != nil {
		return err
	}
	if err := tx.Exec("CREATE EXTENSION IF NOT EXISTS vector").Error; err != nil {
		return err
	}
	return CreateSearchFold(tx)
}

func serializeMigration(tx *gorm.DB, lockTimeout string) error {
	if err := tx.Exec("SELECT pg_advisory_xact_lock($1)", migrationLockID).Error; err != nil {
		return err
	}
	return tx.Exec("SELECT set_config('lock_timeout', $1, true)", lockTimeout).Error
}

func RunMigrations(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := prepareMigration(tx); err != nil {
			return err
		}

		if err := tx.AutoMigrate(
			&schema.Agent{},
			&schema.ConversationMessage{},
			&schema.UnattributedServiceMessage{},
			&schema.AIChatThread{},
			&schema.AIChatMessage{},
			&schema.AIChatQueuedMessage{},
			&schema.AIUsageRecord{},
			&schema.Media{},
			&schema.User{},
			&schema.Address{},
			&schema.CEP{},
			&schema.WhatsAppTemplate{},
			&schema.PasswordResetToken{},
			&schema.SystemConfig{},
			&schema.Customer{},
			&schema.WhatsAppCampaign{},
			&schema.CallRecording{},
			&schema.Call{},
			&schema.Lead{},
			&schema.LeadEvent{},
			&schema.LeadPhone{},
			&schema.LeadAddress{},
			&schema.LeadRelation{},
			&schema.LeadArea{},
			&schema.LeadCompany{},
			&schema.CustomerPortfolioEntry{},
			&schema.LeadImport{},
			&schema.LeadImportIssue{},
			&schema.LeadImportLink{},
			&schema.GeoCEPPoint{},
			&schema.GeoCity{},
			&schema.GeoDistrictPoint{},
			&schema.GeoCEPStreet{},
			&schema.GeoReferenceLoad{},
			&schema.LeadActionRun{},
			&schema.LeadSelectionSnapshot{},
			&schema.CallList{},
			&schema.CallListItem{},
			&schema.WhatsAppCampaignEntry{},
			&schema.WhatsAppCampaignWavePolicy{},
			&schema.WhatsAppCampaignWaveState{},
			&schema.WhatsAppCampaignWave{},
			&schema.WhatsAppBusinessPhoneNumber{},
			&schema.WhatsAppQualityChange{},
			&schema.WhatsAppMessageStatusEvent{},
			&schema.WhatsAppTemplateHourlyStat{},
			&schema.WhatsAppTemplateVersion{},
			&schema.WhatsAppTemplateEdit{},
			&schema.WhatsAppOptOutEvent{},
			&schema.WhatsAppBusinessAccount{},
			&schema.WhatsAppForm{},
			&schema.WhatsAppFormVersion{},
			&schema.WhatsAppFormPublication{},
			&schema.WhatsAppFormSend{},
			&schema.WhatsAppFormResponse{},
			&schema.Balance{},
			&schema.BalanceTransaction{},
			&schema.WorkspaceTemplateAccess{},
			&schema.WorkspacePhoneAccess{},
			&schema.LeadMessageWindow{},
			&schema.WhatsAppCallPermission{},
			&schema.LeadCampaignSend{},
			&schema.ConversationMedia{},
			&schema.ConversationAdOrigin{},
			&schema.Stage{},
			&schema.EntryStage{},
			&schema.Pipeline{},
			&schema.SavedView{},
			&schema.Label{},
			&schema.EntryLabel{},
			&schema.MessageShortcut{},
			&schema.ScheduledMessage{},
			&schema.WhatsAppTemplateSend{},
			&schema.LeadMemory{},
			&schema.StageGroup{},
			&schema.StageGroupItem{},
			&schema.LabelGroup{},
			&schema.LabelGroupItem{},
			&schema.Workspace{},
			&schema.WorkspaceCustomRole{},
			&schema.WorkspaceMember{},
			&schema.WorkspaceMemberPermission{},
			&schema.WorkspaceInvite{},
			&schema.WorkspaceResourceAssignment{},
			&schema.InboxAssignment{},
			&schema.InboxRoundRobinState{},
			&schema.KnowledgeBase{},
			&schema.RAGDocument{},
			&schema.RAGChunk{},
			&schema.AgentKnowledgeBase{},
			&schema.PricingItem{},
			&schema.PricingAuditLog{},
			&schema.Invoice{},
			&schema.InvoiceSupersededCharge{},
			&schema.CallBillingRecord{},
			&schema.ConversationEvent{},
			&schema.WorkspaceConfig{},
			&schema.WorkspaceMonthlySendCap{},
			&schema.WorkspaceMonthlySendSlot{},
			&schema.Issue{},
			&schema.IssueResponse{},
			&schema.WorkflowSchema{},
			&schema.WorkflowVersionSchema{},
			&schema.WorkflowRunSchema{},
			&schema.WorkflowRunLogSchema{},
			&schema.WorkflowWebhookSchema{},
			&schema.WorkflowWebhookDeliverySchema{},
			&schema.AIToolCallSchema{},
			&schema.WorkflowRuntimeAlertSchema{},
			&schema.WorkflowSecret{},
			&schema.BuilderSessionSchema{},
			&schema.BuilderMessageSchema{},
			&schema.CalendarEvent{},
			&schema.GoogleCalendarConnection{},
			&schema.CalendarWatchChannel{},
			&schema.CalendarSyncState{},
			&schema.WorkspaceDepartment{},
			&schema.WorkspaceDepartmentMember{},
			&schema.WorkspacePlanDefinition{},
			&schema.WorkspaceSubscription{},
			&schema.PlanPricingItem{},
			&schema.PlanVisibilityEntry{},
			&schema.WorkspaceAddonDefinition{},
			&schema.WorkspaceAddonSubscription{},
			&schema.Session{},
			&schema.Affiliate{},
			&schema.AffiliateReferral{},
			&schema.AffiliateEarning{},
			&schema.MCPBuiltinBinding{},
			&schema.MCPRemoteServer{},
			&schema.MCPCachedTool{},
			&schema.MCPCollection{},
			&schema.MCPCollectionMember{},
			&schema.AgentMCPCollection{},
			&schema.Opportunity{},
			&schema.DealAutomation{},
			&schema.LiveDecisionRecord{},
			&schema.ConversationLiveRead{},
			&schema.OpportunityConversation{},
			&schema.OpportunityEvent{},
			&schema.CustomFieldDefinition{},
			&schema.AssignmentHistory{},
			&schema.AIAttendanceSession{},
			&schema.QueueEvent{},
			&schema.AttendanceTarget{},
			&schema.ReportJob{},
			&schema.AgentPresenceInterval{},
			&schema.TelemetryDedupe{},
			&schema.ShortLink{},
			&schema.ShortLinkClick{},
			&schema.ShortLinkDailyStat{},
			&schema.InstagramAccount{},
			&schema.InstagramContact{},
			&schema.InstagramConversation{},
			&schema.InstagramMedia{},
			&schema.InstagramComment{},
			&schema.MetaDataDeletionRequest{},
			&schema.FacebookGrant{},
			&schema.FacebookPage{},
			&schema.FacebookContact{},
			&schema.FacebookConversation{},
			&schema.FacebookPost{},
			&schema.FacebookPublishJob{},
			&schema.MediaGenerationJob{},
			&schema.StudioProject{},
			&schema.StudioAnimationPreset{},
			&schema.StudioCapabilityReport{},
			&schema.FacebookComment{},
			&schema.AdGrant{},
			&schema.AdAccount{},
			&schema.AdObject{},
			&schema.AdInsightDaily{},
			&schema.AdPublishJob{},
			&schema.AdSavedAudience{},
			&schema.AdDraft{},
			&schema.AdSavedReport{},
			&schema.AdReportExport{},
			&schema.AdPageWhatsAppLink{},
			&schema.AdLeadForm{},
			&schema.AdFormLead{},
			&schema.AdConversionSettings{},
			&schema.AdConversionRecord{},
			&schema.CommentRule{},
			&schema.CommentPrivateReply{},
			&schema.CommentRuleExecution{},
			&schema.AudienceAnalysis{},
			&schema.AudienceSettings{},
			&schema.AudienceContainerSettings{},
			&schema.AudienceAlertRule{},
			&schema.AudienceAuthor{},
			&schema.AudienceRollup{},
			&schema.AudienceBatch{},
			&schema.AudienceBackfill{},
			&schema.TelegramAccount{},
			&schema.SIPTrunk{},
			&schema.CallQueue{},
			&schema.CallTransfer{},
			&schema.CallRoutingSettings{},
			&schema.TelegramContact{},
			&schema.TelegramConversation{},
			&schema.TelegramDeepLink{},
			&schema.TelegramFileCache{},
			&schema.MercadoLivreAccount{},
			&schema.MercadoLivreContact{},
			&schema.MercadoLivreConversation{},
			&schema.MercadoLivreListing{},
			&schema.MercadoLivreListingSync{},
			&schema.MercadoLivreQuestion{},
			&schema.WebchatWidget{},
			&schema.WebchatVisitor{},
			&schema.WebchatConversation{},
			&schema.ConversationDelegation{},
			&schema.ConversationPersonalMark{},
			&schema.ConversationNote{},
			&schema.TeamChatThread{},
			&schema.TeamChatMember{},
			&schema.TeamChatMessage{},
			&schema.TeamChatScheduledMessage{},
			&schema.UnofficialWhatsAppServer{},
			&schema.UnofficialWhatsAppInstance{},
			&schema.UnofficialWhatsAppContact{},
			&schema.UnofficialWhatsAppConversation{},
			&schema.UnofficialWhatsAppGroup{},
			&schema.UnofficialWhatsAppGroupParticipant{},
			&schema.UnofficialWhatsAppWorkspaceGroup{},
			&schema.UnofficialWhatsAppGroupMember{},
			&schema.UnofficialWhatsAppGroupMemberEvent{},
			&schema.UnofficialWhatsAppGroupAnalysis{},
			&schema.UnofficialWhatsAppGroupAnalysisRun{},
			&schema.UnofficialWhatsAppGroupJoin{},
			&schema.UnofficialWhatsAppCampaign{},
			&schema.UnofficialWhatsAppCampaignEntry{},
			&schema.UnofficialWhatsAppHistorySync{},
			&schema.WebhookProcessedEvent{},
			&schema.TemplateButtonAction{},
			&schema.TemplateExitSettings{},
			&schema.WhatsAppCampaignButtonRoutes{},
			&schema.LeadExitRun{},
			&schema.Notification{},
			&schema.NotificationRecipient{},
			&schema.NotificationPreference{},
			&schema.NotificationBroadcast{},
			&schema.NotificationEpisode{},
		); err != nil {
			return err
		}

		if err := tx.AutoMigrate(schema.TikTokModels()...); err != nil {
			return err
		}

		if err := tx.AutoMigrate(schema.EmailSendingModels()...); err != nil {
			return err
		}

		if err := tx.AutoMigrate(schema.EmailCampaignModels()...); err != nil {
			return err
		}

		if err := tx.AutoMigrate(schema.EmailInboxModels()...); err != nil {
			return err
		}

		if err := tx.AutoMigrate(schema.EmailAPIModels()...); err != nil {
			return err
		}

		if err := tx.AutoMigrate(schema.CarrierMessagingModels()...); err != nil {
			return err
		}

		if err := tx.AutoMigrate(schema.CarrierCampaignModels()...); err != nil {
			return err
		}

		if err := tx.AutoMigrate(schema.CampaignDraftModels()...); err != nil {
			return err
		}

		if err := tx.AutoMigrate(schema.CampaignAudienceModels()...); err != nil {
			return err
		}

		if err := tx.AutoMigrate(schema.GoogleModels()...); err != nil {
			return err
		}

		if err := tx.AutoMigrate(schema.GoogleSheetModels()...); err != nil {
			return err
		}

		if err := tx.AutoMigrate(schema.ChoiceStatsModels()...); err != nil {
			return err
		}

		if err := runDataRepairs(tx); err != nil {
			return err
		}

		return createSchemaConstraints(tx)
	})
}
