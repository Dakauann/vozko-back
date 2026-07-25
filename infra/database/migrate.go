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
			&schema.Analysis{},
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
			&schema.StageGroup{},
			&schema.StageGroupItem{},
			&schema.LabelGroup{},
			&schema.LabelGroupItem{},
			&schema.SIPTrunk{},
			&schema.Branch{},
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
			&schema.CallRouletteState{},
			&schema.CallRouletteAssignment{},
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
		); err != nil {
			return err
		}

		if err := tx.Exec(`
			CREATE UNIQUE INDEX IF NOT EXISTS idx_short_links_code_active
			ON short_links (code)
			WHERE deleted_at IS NULL
		`).Error; err != nil {
			return err
		}

		if err := tx.Exec(`
			CREATE UNIQUE INDEX IF NOT EXISTS idx_workspace_subscriptions_current
			ON workspace_subscriptions (workspace_id)
			WHERE status IN ('active', 'cancelled')
		`).Error; err != nil {
			return err
		}

		if err := tx.Exec(`
			CREATE UNIQUE INDEX IF NOT EXISTS idx_addon_subscription_current
			ON workspace_addon_subscriptions (workspace_id, addon_definition_id)
			WHERE status = 'active'
		`).Error; err != nil {
			return err
		}

		if err := tx.Exec(`
			CREATE UNIQUE INDEX IF NOT EXISTS ux_invoices_idempotency_key
			ON invoices (idempotency_key)
			WHERE idempotency_key <> ''
		`).Error; err != nil {
			return err
		}

		if err := tx.Exec(`
			CREATE UNIQUE INDEX IF NOT EXISTS ux_leads_workspace_number
			ON leads (workspace_id, number)
			WHERE deleted_at IS NULL
		`).Error; err != nil {
			return err
		}

		if err := tx.Exec(`
			CREATE UNIQUE INDEX IF NOT EXISTS ux_branches_sip_user
			ON branches (sip_user)
		`).Error; err != nil {
			return err
		}

		tx.Exec(`
			CREATE INDEX IF NOT EXISTS idx_rag_chunks_embedding_hnsw
			ON rag_chunks USING hnsw (embedding vector_cosine_ops)
			WITH (m = 16, ef_construction = 64)
		`) //nolint:errcheck

		return nil
	})
}
