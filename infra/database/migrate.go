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

		if err := renameCommentAnalysisToAudience(tx); err != nil {
			return err
		}

		if err := renameImageGenerationToMedia(tx); err != nil {
			return err
		}

		if err := tx.AutoMigrate(
			&schema.Category{},
			&schema.Agent{},
			&schema.ConversationMessage{},
			&schema.UnattributedServiceMessage{},
			&schema.AIChatThread{},
			&schema.AIChatMessage{},
			&schema.AIUsageRecord{},
			&schema.Product{},
			&schema.Media{},
			&schema.User{},
			&schema.OptionType{},
			&schema.OptionValue{},
			&schema.Variant{},
			&schema.VariantOption{},
			&schema.VariantMedia{},
			&schema.Cart{},
			&schema.CartItem{},
			&schema.CartItemOption{},
			&schema.Address{},
			&schema.CEP{},
			&schema.Order{},
			&schema.OrderItem{},
			&schema.OrderItemOption{},
			&schema.Payment{},
			&schema.PaymentSplit{},
			&schema.VariantStockAdjustment{},
			&schema.Ticket{},
			&schema.TicketDocument{},
			&schema.ShippingProviderAccount{},
			&schema.InsuranceQuotation{},
			&schema.InsuranceQuote{},
			&schema.WhatsAppTemplate{},
			&schema.PasswordResetToken{},
			&schema.SystemConfig{},
			&schema.Customer{},
			&schema.Property{},
			&schema.Shop{},
			&schema.WhatsAppCampaign{},
			&schema.CallRecording{},
			&schema.Call{},
			&schema.Lead{},
			&schema.LeadEvent{},
			&schema.LeadPhone{},
			&schema.LeadAddress{},
			&schema.LeadRelation{},
			&schema.LeadArea{},
			&schema.LeadImport{},
			&schema.LeadImportIssue{},
			&schema.LeadImportLink{},
			&schema.GeoCEPPoint{},
			&schema.GeoCity{},
			&schema.GeoDistrictPoint{},
			&schema.GeoCEPStreet{},
			&schema.GeoReferenceLoad{},
			&schema.GeocodingSettings{},
			&schema.GeocodingUsage{},
			&schema.GeocodingUsageMonth{},
			&schema.GeocodeCache{},
			&schema.LeadActionRun{},
			&schema.LeadSelectionSnapshot{},
			&schema.CallList{},
			&schema.CallListItem{},
			&schema.WhatsAppCampaignEntry{},
			&schema.WhatsAppBusinessPhoneNumber{},
			&schema.WhatsAppBusinessAccount{},
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
			&schema.CallBillingRecord{},
			&schema.ConversationEvent{},
			&schema.WorkspaceConfig{},
			&schema.WorkspaceMonthlySendCap{},
			&schema.WorkspaceMonthlySendSlot{},
			&schema.Issue{},
			&schema.IssueResponse{},
			&schema.WorkflowSchema{},
			&schema.WorkflowRunSchema{},
			&schema.WorkflowRunLogSchema{},
			&schema.WorkflowWebhookSchema{},
			&schema.BuilderSessionSchema{},
			&schema.BuilderMessageSchema{},
			&schema.CalendarEvent{},
			&schema.GoogleCalendarConnection{},
			&schema.CalendarWatchChannel{},
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
			&schema.ProcessedWebhookEvent{},
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
			&schema.InstagramCommentRule{},
			&schema.InstagramPrivateReply{},
			&schema.MetaDataDeletionRequest{},
			&schema.FacebookGrant{},
			&schema.FacebookPage{},
			&schema.FacebookContact{},
			&schema.FacebookConversation{},
			&schema.FacebookPost{},
			&schema.FacebookPublishJob{},
			&schema.MediaGenerationJob{},
			&schema.StudioProject{},
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
			&schema.WebchatWidget{},
			&schema.WebchatVisitor{},
			&schema.WebchatConversation{},
			&schema.ConversationDelegation{},
			&schema.UnofficialWhatsAppServer{},
			&schema.UnofficialWhatsAppInstance{},
			&schema.UnofficialWhatsAppContact{},
			&schema.UnofficialWhatsAppConversation{},
			&schema.UnofficialWhatsAppGroup{},
			&schema.UnofficialWhatsAppGroupParticipant{},
			&schema.UnofficialWhatsAppCampaign{},
			&schema.UnofficialWhatsAppCampaignEntry{},
			&schema.UnofficialWhatsAppHistorySync{},
			&schema.WebhookProcessedEvent{},
		); err != nil {
			return err
		}

		if err := runDataRepairs(tx); err != nil {
			return err
		}

		return createSchemaConstraints(tx)
	})
}
