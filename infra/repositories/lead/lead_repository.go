package lead

import (
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/domain/lead"
	"vozko/domain/shared"
	"vozko/infra/database/schema"
	infracrmfilter "vozko/infra/repositories/crmfilter"
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

// listQuery is the compiled read query: the WHERE fragment every lead read
// path shares, plus its positional args.
//
// One builder, four consumers (page, count, facet aggregate, facet
// breakdowns). The version this replaced hand-wrote the same six predicates in
// two functions, and only one of them had ever learned about created/age
// ranges — the plain List() silently ignored them.
type listQuery struct {
	desc  infracrmfilter.LeadDescriptor
	where string
	args  []interface{}
}

func (r *repository) compile(input lead.ListLeadsInput) (*listQuery, error) {
	workspaceID := strings.TrimSpace(input.WorkspaceID)
	if workspaceID == "" {
		return nil, lead.ErrLeadWorkspaceRequired
	}

	desc := infracrmfilter.LeadDescriptor{Alias: "leads", WorkspaceID: workspaceID}
	frag, args, err := infracrmfilter.Compile(input.Filter, desc, 0)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", lead.ErrLeadFilterInvalid, err)
	}

	// Raw SQL bypasses GORM's soft-delete scope, so the tenant boundary and the
	// deleted_at guard are stated explicitly here rather than inherited.
	where := "leads.workspace_id = ? AND leads.deleted_at IS NULL"
	all := []interface{}{workspaceID}
	if frag != "" {
		where += " AND (" + frag + ")"
		all = append(all, args...)
	}

	return &listQuery{desc: desc, where: where, args: all}, nil
}

// filteredIDs is the lead-id subquery the grouped facet counts join against.
func (q *listQuery) filteredIDs() string {
	return "SELECT leads.id FROM leads WHERE " + q.where
}

// sortExpressions maps a client sort key onto the SQL it orders by. Computed
// keys resolve to the SELECT alias rather than repeating the expression, so
// Postgres evaluates each subquery once per row instead of twice.
func sortExpressions() map[lead.SortKey]string {
	return map[lead.SortKey]string{
		lead.SortCreatedAt: "leads.created_at",
		lead.SortUpdatedAt: "leads.updated_at",
		// NULLIF so unnamed leads sort as missing (and land last) instead of
		// clustering at the top of an A→Z list under the empty string.
		lead.SortName:           "NULLIF(leads.name, '')",
		lead.SortNumber:         "leads.number",
		lead.SortAge:            "leads.age",
		lead.SortLastActivityAt: "last_activity_at",
		lead.SortCampaigns:      "campaign_count",
		lead.SortMemories:       "memory_count",
		lead.SortLastMemoryAt:   "last_memory_at",
	}
}

// orderBy renders the ORDER BY clause.
//
// Two non-obvious rules, both about pagination being trustworthy:
//
//   - NULLS LAST on every key. Postgres puts NULLs FIRST on DESC, so "most
//     recent activity first" would otherwise open on the leads that have never
//     done anything.
//   - leads.id as the final tiebreaker, always. Without it, rows tied on the
//     sort key (every lead imported in the same second, every lead with zero
//     campaigns) come back in an undefined order, and the same lead can appear
//     on page 2 and page 3 while another appears on neither.
func orderBy(sorts []shared.Sort) string {
	exprs := sortExpressions()
	parts := make([]string, 0, len(sorts)+1)
	seen := map[lead.SortKey]struct{}{}

	for _, s := range sorts {
		key, ok := lead.ParseSortKey(s.Field)
		if !ok {
			continue
		}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}

		direction := "ASC"
		if s.Direction == shared.SortDesc {
			direction = "DESC"
		}
		parts = append(parts, exprs[key]+" "+direction+" NULLS LAST")
	}

	if len(parts) == 0 {
		parts = append(parts, "leads.created_at DESC NULLS LAST")
	}
	return strings.Join(parts, ", ") + ", leads.id DESC"
}

// leadListRow is one row of the page query: the lead columns plus the derived
// summary, resolved in the same pass so a page of 100 leads costs one query
// instead of one plus three batched follow-ups.
type leadListRow struct {
	ID                string
	WorkspaceID       string
	Number            string
	Name              string
	ProfilePictureURL string
	Age               *int
	Blocked           bool
	BlockedAt         *time.Time
	BlockedBy         *string
	CreatedAt         time.Time
	UpdatedAt         time.Time

	CampaignCount   int
	MemoryCount     int
	LastActivityAt  *time.Time
	LastMemoryAt    *time.Time
	WindowOpen      bool
	WindowExpiresAt *time.Time
}

func (row *leadListRow) toDomain() *lead.Lead {
	l := &lead.Lead{
		ID:                row.ID,
		WorkspaceID:       row.WorkspaceID,
		Number:            row.Number,
		Name:              row.Name,
		ProfilePictureURL: row.ProfilePictureURL,
		Age:               row.Age,
		Blocked:           row.Blocked,
		BlockedBy:         row.BlockedBy,
		CreatedAt:         row.CreatedAt,
		UpdatedAt:         row.UpdatedAt,
	}
	if row.BlockedAt != nil {
		l.BlockedAt = *row.BlockedAt
	}
	return l
}

func (row *leadListRow) toSummary() *lead.LeadSummary {
	summary := &lead.LeadSummary{
		WhatsAppCampaigns:  row.CampaignCount,
		TotalCampaigns:     row.CampaignCount,
		LastActivityAt:     row.LastActivityAt,
		WhatsAppWindowOpen: row.WindowOpen,
		Memories:           row.MemoryCount,
		LastMemoryAt:       row.LastMemoryAt,
	}
	// The expiry is only meaningful while the window is open; reporting a past
	// one would render as an expired countdown next to a "closed" badge.
	if row.WindowOpen {
		summary.WindowExpiresAt = row.WindowExpiresAt
	}
	return summary
}

// selectList is the projection shared by every page read.
func (q *listQuery) selectList() string {
	d := q.desc
	return "leads.id, leads.workspace_id, leads.number, leads.name, leads.profile_picture_url, " +
		"leads.age, leads.blocked, leads.blocked_at, leads.blocked_by, leads.created_at, leads.updated_at, " +
		d.CampaignCountExpr() + " AS campaign_count, " +
		d.MemoryCountExpr() + " AS memory_count, " +
		d.LastActivityExpr() + " AS last_activity_at, " +
		d.LastMemoryAtExpr() + " AS last_memory_at, " +
		d.WindowOpenExpr() + " AS window_open, " +
		d.WindowExpiresAtExpr() + " AS window_expires_at"
}

func (r *repository) countLeads(q *listQuery) (int64, error) {
	var total int64
	if err := r.db.Raw("SELECT COUNT(*) FROM leads WHERE "+q.where, q.args...).Scan(&total).Error; err != nil {
		return 0, err
	}
	return total, nil
}

func (r *repository) fetchPage(q *listQuery, opts shared.QueryOptions) ([]leadListRow, error) {
	pagination := shared.NormalizePagination(opts.Pagination)

	sql := "SELECT " + q.selectList() +
		" FROM leads WHERE " + q.where +
		" ORDER BY " + orderBy(opts.Sorts) +
		" LIMIT ? OFFSET ?"

	args := append(append([]interface{}{}, q.args...), pagination.PageSize, pagination.Offset())

	var rows []leadListRow
	if err := r.db.Raw(sql, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *repository) List(input lead.ListLeadsInput) (*shared.PaginatedResult[*lead.Lead], error) {
	q, err := r.compile(input)
	if err != nil {
		return nil, err
	}

	total, err := r.countLeads(q)
	if err != nil {
		return nil, err
	}

	rows, err := r.fetchPage(q, input.Options)
	if err != nil {
		return nil, err
	}

	items := make([]*lead.Lead, len(rows))
	for i := range rows {
		items[i] = rows[i].toDomain()
	}

	return shared.NewPaginatedResult(items, input.Options.Pagination, total), nil
}

func (r *repository) ListWithSummary(input lead.ListLeadsInput) (*shared.PaginatedResult[*lead.LeadWithSummary], error) {
	q, err := r.compile(input)
	if err != nil {
		return nil, err
	}

	total, err := r.countLeads(q)
	if err != nil {
		return nil, err
	}

	rows, err := r.fetchPage(q, input.Options)
	if err != nil {
		return nil, err
	}

	items := make([]*lead.LeadWithSummary, len(rows))
	for i := range rows {
		items[i] = &lead.LeadWithSummary{
			Lead:    rows[i].toDomain(),
			Summary: rows[i].toSummary(),
		}
	}

	return shared.NewPaginatedResult(items, input.Options.Pagination, total), nil
}

// facetRow is one (bucket, count) pair of a grouped breakdown.
type facetRow struct {
	Key   string
	Count int64
}

// groupedFacet counts DISTINCT leads per bucket over the filtered set. source
// is a FROM fragment exposing lead_id and the bucket column.
func (r *repository) groupedFacet(q *listQuery, source, leadIDCol, bucketCol, extra string) map[string]int64 {
	conditions := leadIDCol + " IN (" + q.filteredIDs() + ")"
	if extra != "" {
		conditions += " AND " + extra
	}
	sql := "SELECT " + bucketCol + " AS key, COUNT(DISTINCT " + leadIDCol + ") AS count" +
		" FROM " + source + " WHERE " + conditions + " GROUP BY " + bucketCol

	var rows []facetRow
	if err := r.db.Raw(sql, q.args...).Scan(&rows).Error; err != nil {
		// A breakdown that cannot be counted is reported as absent, not as a
		// failed list: the rows are already correct, and an empty bucket map
		// renders the filter without counts rather than an error page.
		log.Printf("[lead-facets] grouped facet on %s failed: %v", source, err)
		return map[string]int64{}
	}

	out := make(map[string]int64, len(rows))
	for _, row := range rows {
		key := strings.TrimSpace(row.Key)
		if key == "" {
			continue
		}
		out[key] = row.Count
	}
	return out
}

func (r *repository) Facets(input lead.ListLeadsInput) (*lead.LeadFacets, error) {
	q, err := r.compile(input)
	if err != nil {
		return nil, err
	}
	d := q.desc

	// One pass for the boolean buckets: FILTER re-uses the single scan the
	// total already pays for, so seven counters cost what one does.
	var agg struct {
		Total        int64
		Blocked      int64
		WindowOpen   int64
		WithCampaign int64
		WithMemory   int64
		Named        int64
	}
	sql := "SELECT COUNT(*) AS total," +
		" COUNT(*) FILTER (WHERE leads.blocked) AS blocked," +
		" COUNT(*) FILTER (WHERE " + d.WindowOpenExpr() + ") AS window_open," +
		" COUNT(*) FILTER (WHERE " + d.CampaignCountExpr() + " > 0) AS with_campaign," +
		" COUNT(*) FILTER (WHERE " + d.MemoryCountExpr() + " > 0) AS with_memory," +
		" COUNT(*) FILTER (WHERE NULLIF(leads.name, '') IS NOT NULL) AS named" +
		" FROM leads WHERE " + q.where
	if err := r.db.Raw(sql, q.args...).Scan(&agg).Error; err != nil {
		return nil, err
	}

	// Complements are derived, never counted twice: two COUNT(*) FILTERs that
	// are supposed to add up to the total is a pair that can disagree.
	facets := &lead.LeadFacets{
		Total:           agg.Total,
		Blocked:         agg.Blocked,
		Active:          agg.Total - agg.Blocked,
		WindowOpen:      agg.WindowOpen,
		WindowClosed:    agg.Total - agg.WindowOpen,
		WithCampaign:    agg.WithCampaign,
		WithoutCampaign: agg.Total - agg.WithCampaign,
		WithMemory:      agg.WithMemory,
		WithoutMemory:   agg.Total - agg.WithMemory,
		Named:           agg.Named,
		Unnamed:         agg.Total - agg.Named,
	}

	facets.MemoryCategories = r.groupedFacet(q, "lead_memories lm_g", "lm_g.lead_id", "lm_g.category", "lm_g.deleted_at IS NULL")
	facets.CampaignStatuses = r.groupedFacet(q, "whatsapp_campaign_entries wce_g", "wce_g.lead_id", "wce_g.status", "wce_g.deleted_at IS NULL")
	facets.Channels = r.groupedFacet(q, infracrmfilter.LeadChannelsSource(), "lead_id", "channel", "")

	return facets, nil
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
