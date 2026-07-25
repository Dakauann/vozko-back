package lead

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/domain/lead"
	"vozko/domain/shared"
	"vozko/infra/database/schema"
)

type repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) lead.Repository {
	return &repository{db: db}
}

func (r *repository) scope(workspaceID string) *gorm.DB {
	return r.db.Where("workspace_id = ?", workspaceID)
}

func (r *repository) Create(l *lead.Lead) error {
	if l == nil {
		return lead.ErrLeadRequired
	}
	l.Normalize()
	if err := l.Validate(); err != nil {
		return err
	}
	if l.ID == "" {
		l.ID = uuid.New().String()
	}
	schemaLead := toSchema(l)
	return r.db.Create(&schemaLead).Error
}

func (r *repository) FindByID(workspaceID, id string) (*lead.Lead, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	id = strings.TrimSpace(id)
	if workspaceID == "" {
		return nil, lead.ErrLeadWorkspaceRequired
	}
	if id == "" {
		return nil, lead.ErrLeadRequired
	}

	var schemaLead schema.Lead
	if err := r.scope(workspaceID).Where("id = ?", id).First(&schemaLead).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, lead.ErrLeadNotFound
		}
		return nil, err
	}

	return toDomain(&schemaLead), nil
}

func (r *repository) FindByNumber(workspaceID, number string) (*lead.Lead, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, lead.ErrLeadWorkspaceRequired
	}
	normalized := lead.NormalizeNumber(number)
	if normalized == "" {
		return nil, lead.ErrLeadInvalid
	}

	phoneFormats := []string{normalized}
	if alternate := lead.GetAlternatePhoneFormat(normalized); alternate != "" {
		phoneFormats = append(phoneFormats, alternate)
	}

	var schemaLead schema.Lead
	if err := r.scope(workspaceID).Where("number IN ?", phoneFormats).First(&schemaLead).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, lead.ErrLeadNotFound
		}
		return nil, err
	}

	return toDomain(&schemaLead), nil
}

func (r *repository) FindByIDs(workspaceID string, ids []string) ([]*lead.Lead, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, lead.ErrLeadWorkspaceRequired
	}
	if len(ids) == 0 {
		return []*lead.Lead{}, nil
	}

	var schemaLeads []schema.Lead
	if err := r.scope(workspaceID).Where("id IN ?", ids).Find(&schemaLeads).Error; err != nil {
		return nil, err
	}

	leads := make([]*lead.Lead, len(schemaLeads))
	for i, sl := range schemaLeads {
		leads[i] = toDomain(&sl)
	}

	return leads, nil
}

func (r *repository) FindOrCreate(workspaceID, number string, update lead.LeadUpdate) (*lead.Lead, bool, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, false, lead.ErrLeadWorkspaceRequired
	}
	normalized := lead.NormalizeNumber(number)
	if normalized == "" {
		return nil, false, lead.ErrLeadInvalid
	}

	phoneFormats := []string{normalized}
	if alternate := lead.GetAlternatePhoneFormat(normalized); alternate != "" {
		phoneFormats = append(phoneFormats, alternate)
	}

	var schemaLead schema.Lead
	err := r.scope(workspaceID).Where("number IN ?", phoneFormats).First(&schemaLead).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		newLead := &lead.Lead{
			ID:          uuid.New().String(),
			WorkspaceID: workspaceID,
			Number:      normalized,
			Name:        update.Name,
			Age:         update.Age,
		}

		schemaLead = *toSchema(newLead)
		if err := r.db.Create(&schemaLead).Error; err != nil {
			return nil, false, err
		}
		return toDomain(&schemaLead), true, nil
	}

	if err != nil {
		return nil, false, err
	}

	domainLead := toDomain(&schemaLead)
	domainLead.Merge(update)

	updateData := map[string]interface{}{}
	if update.Name != "" {
		updateData["name"] = domainLead.Name
	}
	if update.Age != nil {
		updateData["age"] = domainLead.Age
	}

	if len(updateData) > 0 {
		if err := r.db.Model(&schemaLead).Updates(updateData).Error; err != nil {
			return nil, false, err
		}
	}

	return domainLead, false, nil
}

func (r *repository) FindOrCreateMany(workspaceID string, inputs []lead.BulkLeadInput) (map[string]*lead.Lead, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, lead.ErrLeadWorkspaceRequired
	}
	if len(inputs) == 0 {
		return make(map[string]*lead.Lead), nil
	}

	normalizedNumbers := make([]string, 0, len(inputs))
	inputByNumber := make(map[string]lead.BulkLeadInput)
	for _, input := range inputs {
		normalized := lead.NormalizeNumber(input.Number)
		if normalized == "" {
			continue
		}
		if _, exists := inputByNumber[normalized]; !exists {
			normalizedNumbers = append(normalizedNumbers, normalized)
			inputByNumber[normalized] = input
		}
	}

	if len(normalizedNumbers) == 0 {
		return make(map[string]*lead.Lead), nil
	}

	result := make(map[string]*lead.Lead)

	allSearchNumbers := make([]string, 0, len(normalizedNumbers)*2)
	for _, number := range normalizedNumbers {
		allSearchNumbers = append(allSearchNumbers, number)
		if alternate := lead.GetAlternatePhoneFormat(number); alternate != "" {
			allSearchNumbers = append(allSearchNumbers, alternate)
		}
	}

	const batchSize = 500
	var existingLeads []schema.Lead
	for i := 0; i < len(allSearchNumbers); i += batchSize {
		end := i + batchSize
		if end > len(allSearchNumbers) {
			end = len(allSearchNumbers)
		}
		batch := allSearchNumbers[i:end]

		var batchLeads []schema.Lead
		if err := r.scope(workspaceID).Where("number IN ?", batch).Find(&batchLeads).Error; err != nil {
			return nil, err
		}
		existingLeads = append(existingLeads, batchLeads...)
	}

	existingByNumber := make(map[string]*schema.Lead)
	for i := range existingLeads {
		existingByNumber[existingLeads[i].Number] = &existingLeads[i]
		if alt := lead.GetAlternatePhoneFormat(existingLeads[i].Number); alt != "" {
			if _, alreadyMapped := existingByNumber[alt]; !alreadyMapped {
				existingByNumber[alt] = &existingLeads[i]
			}
		}
	}

	var newLeadsToCreate []schema.Lead
	for _, number := range normalizedNumbers {
		if existing, found := existingByNumber[number]; found {
			input := inputByNumber[number]
			updates := map[string]interface{}{}

			if existing.Name == "" && input.Name != "" {
				updates["name"] = input.Name
				existing.Name = input.Name
			}

			if input.Age != nil && existing.Age == nil {
				updates["age"] = *input.Age
				existing.Age = input.Age
			}

			if len(updates) > 0 {
				r.db.Model(existing).Updates(updates)
			}

			result[number] = toDomain(existing)
		} else {
			input := inputByNumber[number]
			newLead := schema.Lead{
				ID:          uuid.New().String(),
				WorkspaceID: workspaceID,
				Number:      number,
				Name:        input.Name,
				Age:         input.Age,
			}
			newLeadsToCreate = append(newLeadsToCreate, newLead)
		}
	}

	if len(newLeadsToCreate) > 0 {
		if err := r.db.CreateInBatches(&newLeadsToCreate, batchSize).Error; err != nil {
			return nil, err
		}

		for i := range newLeadsToCreate {
			result[newLeadsToCreate[i].Number] = toDomain(&newLeadsToCreate[i])
		}
	}

	return result, nil
}

func (r *repository) Update(workspaceID, id string, update lead.LeadUpdate) error {
	workspaceID = strings.TrimSpace(workspaceID)
	id = strings.TrimSpace(id)
	if workspaceID == "" {
		return lead.ErrLeadWorkspaceRequired
	}
	if id == "" {
		return lead.ErrLeadRequired
	}

	existingLead, err := r.FindByID(workspaceID, id)
	if err != nil {
		return err
	}

	existingLead.Merge(update)

	updateData := map[string]interface{}{
		"name":                existingLead.Name,
		"age":                 existingLead.Age,
		"profile_picture_url": existingLead.ProfilePictureURL,
		"blocked":             existingLead.Blocked,
		"blocked_by":          existingLead.BlockedBy,
	}
	if existingLead.Blocked && !existingLead.BlockedAt.IsZero() {
		blockedAt := existingLead.BlockedAt
		updateData["blocked_at"] = blockedAt
	} else {
		updateData["blocked_at"] = nil
	}

	return r.scope(workspaceID).Model(&schema.Lead{}).Where("id = ?", id).Updates(updateData).Error
}

func (r *repository) Delete(workspaceID, id string) error {
	workspaceID = strings.TrimSpace(workspaceID)
	id = strings.TrimSpace(id)
	if workspaceID == "" {
		return lead.ErrLeadWorkspaceRequired
	}
	if id == "" {
		return lead.ErrLeadRequired
	}
	return r.scope(workspaceID).Where("id = ?", id).Delete(&schema.Lead{}).Error
}

func (r *repository) List(input lead.ListLeadsInput) (*shared.PaginatedResult[*lead.Lead], error) {
	workspaceID := strings.TrimSpace(input.WorkspaceID)
	if workspaceID == "" {
		return nil, lead.ErrLeadWorkspaceRequired
	}
	input.Options.Pagination = shared.NormalizePagination(input.Options.Pagination)

	query := r.scope(workspaceID).Model(&schema.Lead{})

	if input.Number != "" {
		query = query.Where("number LIKE ?", "%"+input.Number+"%")
	}
	if input.Name != "" {
		query = query.Where("name ILIKE ?", "%"+input.Name+"%")
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}

	offset := (input.Options.Pagination.Page - 1) * input.Options.Pagination.PageSize
	var schemaLeads []schema.Lead
	if err := query.Offset(offset).Limit(input.Options.Pagination.PageSize).Order("created_at DESC").Find(&schemaLeads).Error; err != nil {
		return nil, err
	}

	items := make([]*lead.Lead, len(schemaLeads))
	for i, l := range schemaLeads {
		items[i] = toDomain(&l)
	}

	totalPages := int(total) / input.Options.Pagination.PageSize
	if int(total)%input.Options.Pagination.PageSize > 0 {
		totalPages++
	}

	return &shared.PaginatedResult[*lead.Lead]{
		Items:      items,
		Page:       input.Options.Pagination.Page,
		PageSize:   input.Options.Pagination.PageSize,
		TotalItems: total,
		TotalPages: totalPages,
	}, nil
}

func (r *repository) buildFilteredQuery(workspaceID string, input lead.ListLeadsInput) *gorm.DB {
	query := r.scope(workspaceID).Model(&schema.Lead{})

	if input.Number != "" {
		query = query.Where("number LIKE ?", "%"+input.Number+"%")
	}
	if input.Name != "" {
		query = query.Where("name ILIKE ?", "%"+input.Name+"%")
	}
	if input.CreatedFrom != nil {
		query = query.Where("created_at >= ?", *input.CreatedFrom)
	}
	if input.CreatedTo != nil {
		query = query.Where("created_at <= ?", *input.CreatedTo)
	}
	if input.AgeFrom != nil {
		query = query.Where("age >= ?", *input.AgeFrom)
	}
	if input.AgeTo != nil {
		query = query.Where("age <= ?", *input.AgeTo)
	}
	if input.HasWhatsAppCampaign != nil {
		if *input.HasWhatsAppCampaign {
			query = query.Where("EXISTS (SELECT 1 FROM whatsapp_campaign_entries WHERE whatsapp_campaign_entries.lead_id = leads.id AND whatsapp_campaign_entries.deleted_at IS NULL)")
		} else {
			query = query.Where("NOT EXISTS (SELECT 1 FROM whatsapp_campaign_entries WHERE whatsapp_campaign_entries.lead_id = leads.id AND whatsapp_campaign_entries.deleted_at IS NULL)")
		}
	}

	for _, s := range input.Options.Sorts {
		dir := "ASC"
		if s.Direction == shared.SortDesc {
			dir = "DESC"
		}
		query = query.Order(s.Field + " " + dir)
	}

	return query
}

func (r *repository) fetchLeadSummaryByIDs(leadIDs []string) map[string]*lead.LeadSummary {
	if len(leadIDs) == 0 {
		return nil
	}

	result := make(map[string]*lead.LeadSummary, len(leadIDs))
	for _, id := range leadIDs {
		result[id] = &lead.LeadSummary{}
	}

	type leadCount struct {
		LeadID string
		Count  int
		Last   *time.Time
	}

	var wcCounts []leadCount
	r.db.Table("whatsapp_campaign_entries").
		Select("lead_id, COUNT(*) as count, MAX(updated_at) as last").
		Where("lead_id IN ? AND deleted_at IS NULL", leadIDs).
		Group("lead_id").
		Scan(&wcCounts)
	for _, v := range wcCounts {
		if s, ok := result[v.LeadID]; ok {
			s.WhatsAppCampaigns = v.Count
			if v.Last != nil && (s.LastActivityAt == nil || v.Last.After(*s.LastActivityAt)) {
				s.LastActivityAt = v.Last
			}
		}
	}

	for _, s := range result {
		s.TotalCampaigns = s.WhatsAppCampaigns
	}

	type windowRow struct {
		LeadID        string
		LastMessageAt time.Time
	}
	var windows []windowRow
	r.db.Table("lead_message_windows").
		Select("lead_id, MAX(last_message_at) as last_message_at").
		Where("lead_id IN ?", leadIDs).
		Group("lead_id").
		Scan(&windows)
	for _, w := range windows {
		if s, ok := result[w.LeadID]; ok {
			s.WhatsAppWindowOpen = time.Since(w.LastMessageAt) < 24*time.Hour
			if s.WhatsAppWindowOpen {
				exp := w.LastMessageAt.Add(24 * time.Hour)
				s.WindowExpiresAt = &exp
			}
			if s.LastActivityAt == nil || w.LastMessageAt.After(*s.LastActivityAt) {
				s.LastActivityAt = &w.LastMessageAt
			}
		}
	}

	return result
}

func (r *repository) ListWithSummary(input lead.ListLeadsInput) (*shared.PaginatedResult[*lead.LeadWithSummary], error) {
	workspaceID := strings.TrimSpace(input.WorkspaceID)
	if workspaceID == "" {
		return nil, lead.ErrLeadWorkspaceRequired
	}
	input.Options.Pagination = shared.NormalizePagination(input.Options.Pagination)

	if len(input.Options.Sorts) == 0 {
		input.Options.Sorts = []shared.Sort{{Field: "created_at", Direction: shared.SortDesc}}
	}

	query := r.buildFilteredQuery(workspaceID, input)

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}

	offset := (input.Options.Pagination.Page - 1) * input.Options.Pagination.PageSize
	var schemaLeads []schema.Lead
	if err := query.Offset(offset).Limit(input.Options.Pagination.PageSize).Find(&schemaLeads).Error; err != nil {
		return nil, err
	}

	leadIDs := make([]string, len(schemaLeads))
	for i, l := range schemaLeads {
		leadIDs[i] = l.ID
	}

	summaries := r.fetchLeadSummaryByIDs(leadIDs)

	items := make([]*lead.LeadWithSummary, len(schemaLeads))
	for i, l := range schemaLeads {
		domainLead := toDomain(&l)
		summary := summaries[l.ID]
		if summary == nil {
			summary = &lead.LeadSummary{}
		}
		items[i] = &lead.LeadWithSummary{
			Lead:    domainLead,
			Summary: summary,
		}
	}

	return shared.NewPaginatedResult(items, input.Options.Pagination, total), nil
}

func (r *repository) ResolveCampaignNames(wcIDs []string) map[string]string {
	names := make(map[string]string)

	type nameRow struct {
		ID   string
		Name string
	}

	if len(wcIDs) > 0 {
		var rows []nameRow
		r.db.Table("whatsapp_campaigns").Select("id, name").Where("id IN ?", wcIDs).Scan(&rows)
		for _, row := range rows {
			names["whatsapp:"+row.ID] = row.Name
		}
	}

	return names
}

func toSchema(l *lead.Lead) *schema.Lead {
	s := &schema.Lead{
		ID:                l.ID,
		WorkspaceID:       l.WorkspaceID,
		Number:            l.Number,
		Name:              l.Name,
		ProfilePictureURL: l.ProfilePictureURL,
		Age:               l.Age,
		Blocked:           l.Blocked,
		BlockedBy:         l.BlockedBy,
	}
	if !l.BlockedAt.IsZero() {
		blockedAt := l.BlockedAt
		s.BlockedAt = &blockedAt
	}
	return s
}

func toDomain(l *schema.Lead) *lead.Lead {
	d := &lead.Lead{
		ID:                l.ID,
		WorkspaceID:       l.WorkspaceID,
		Number:            l.Number,
		Name:              l.Name,
		ProfilePictureURL: l.ProfilePictureURL,
		Age:               l.Age,
		Blocked:           l.Blocked,
		BlockedBy:         l.BlockedBy,
		CreatedAt:         l.CreatedAt,
		UpdatedAt:         l.UpdatedAt,
	}
	if l.BlockedAt != nil {
		d.BlockedAt = *l.BlockedAt
	}
	return d
}
