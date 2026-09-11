package database

import (
	"vozko/infra/database/schema"

	"gorm.io/gorm"
)

const migrationLockID = 123456789

// RunMigrations brings the database up to the schema the code expects.
//
// It is schema-only: AutoMigrate creates/extends every table from the Go
// structs, and the statements after it add the partial and unique indexes
// GORM's struct tags cannot express. Everything is idempotent, so this runs on
// every boot and is a no-op once the database matches.
func RunMigrations(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock($1)", migrationLockID).Error; err != nil {
			return err
		}

		if err := tx.Exec("CREATE EXTENSION IF NOT EXISTS vector").Error; err != nil {
			return err
		}

		// The same rename, in the schema itself.
		//
		// This MUST run before AutoMigrate. AutoMigrate creates whatever it does
		// not find and never renames, so if it ran first it would create eight
		// empty audience_* tables beside the populated comment_analysis_* ones,
		// and every customer's history would still be in the old tables while
		// the application read the new, empty ones. The same applies to each
		// column.
		if err := renameCommentAnalysisToAudience(tx); err != nil {
			return err
		}

		if err := tx.AutoMigrate(
			&schema.Category{},
			&schema.Agent{},
			&schema.ConversationMessage{},
			&schema.AIChatThread{},
			&schema.AIChatMessage{},
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
			&schema.BusinessMetric{},
			&schema.Property{},
			&schema.Shop{},
			&schema.WhatsAppCampaign{},
			&schema.CallRecording{},
			&schema.Call{},
			&schema.Lead{},
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
			&schema.SupportInbox{},
			&schema.SupportEntry{},
			&schema.SupportSession{},
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
			&schema.OpportunityConversation{},
			&schema.CustomFieldDefinition{},
			&schema.AssignmentHistory{},
			&schema.AIAttendanceSession{},
			&schema.QueueEvent{},
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
			&schema.AudienceAnalysis{},
			&schema.AudienceSettings{},
			&schema.AudienceContainerSettings{},
			&schema.AudienceAlertRule{},
			&schema.AudienceAuthor{},
			&schema.AudienceRollup{},
			&schema.AudienceBatch{},
			&schema.AudienceBackfill{},
			&schema.TelegramAccount{},
			&schema.TelegramContact{},
			&schema.TelegramConversation{},
			&schema.TelegramDeepLink{},
			&schema.TelegramFileCache{},
			&schema.UnofficialWhatsAppServer{},
			&schema.UnofficialWhatsAppInstance{},
			&schema.UnofficialWhatsAppContact{},
			&schema.UnofficialWhatsAppConversation{},
			&schema.UnofficialWhatsAppGroup{},
			&schema.UnofficialWhatsAppGroupParticipant{},
			// Campaigns come after the instance and contact tables they
			// reference, so the FK targets exist when these are created.
			&schema.UnofficialWhatsAppCampaign{},
			&schema.UnofficialWhatsAppCampaignEntry{},
			&schema.WebhookProcessedEvent{},
		); err != nil {
			return err
		}

		// Data repairs run BEFORE the constraints, because a constraint that
		// codifies a rule the existing rows break cannot be created until they
		// stop breaking it. Each one is idempotent and a no-op on a database
		// that never had the defect.
		if err := runDataRepairs(tx); err != nil {
			return err
		}

		// Every index in this codebase is declared in indexes.go. These run
		// inside the migration transaction because they are constraints rather
		// than tuning, so a failure has to abort rather than warn.
		return createSchemaConstraints(tx)
	})
}
