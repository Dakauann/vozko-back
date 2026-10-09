package lead

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"vozko/domain/crmfilter"
	"vozko/domain/lead"
	"vozko/domain/leadarea"
	"vozko/domain/shared"
	"vozko/infra/database/schema"
	infracrmfilter "vozko/infra/repositories/crmfilter"
)

type listQuery struct {
	desc       infracrmfilter.LeadDescriptor
	where      string
	args       []interface{}
	areas      bool
	areaBounds []crmfilter.AreaBounds
	rank       string
	rankArgs   []interface{}

	cityPlace     placeMatch
	districtPlace placeMatch
}

func (r *repository) compile(input lead.ListLeadsInput) (*listQuery, error) {
	workspaceID := strings.TrimSpace(input.WorkspaceID)
	if workspaceID == "" {
		return nil, lead.ErrLeadWorkspaceRequired
	}

	if err := lead.ValidateFilter(input.Filter); err != nil {
		return nil, fmt.Errorf("%w: %w", lead.ErrLeadFilterInvalid, err)
	}
	desc := infracrmfilter.LeadDescriptor{Alias: "leads", WorkspaceID: workspaceID, Today: input.Today}
	frag, args, err := infracrmfilter.Compile(input.Filter, desc, 0)
	if errors.Is(err, crmfilter.ErrBirthdayClockMissing) {
		return nil, fmt.Errorf("lead filter compiled without the workspace day: %w", err)
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %w", lead.ErrLeadFilterInvalid, err)
	}

	where := "leads.workspace_id = ? AND leads.deleted_at IS NULL"
	all := []interface{}{workspaceID}
	if frag != "" {
		where += " AND (" + frag + ")"
		all = append(all, args...)
	}

	rank, rankArgs, _ := desc.SearchOrder(input.Filter)
	return &listQuery{desc: desc, where: where, args: all, areas: leadarea.HasArea(input.Filter), areaBounds: input.Filter.BoundAreas(), rank: rank, rankArgs: rankArgs}, nil
}

func (q *listQuery) filteredIDs() string {
	return "SELECT leads.id FROM leads WHERE " + q.where
}

func sortExpressions(d infracrmfilter.LeadDescriptor) map[lead.SortKey]string {
	return map[lead.SortKey]string{
		lead.SortCreatedAt:      "leads.created_at",
		lead.SortUpdatedAt:      "leads.updated_at",
		lead.SortName:           "NULLIF(leads.name, '')",
		lead.SortNumber:         "leads.number",
		lead.SortAge:            "leads.age",
		lead.SortLastActivityAt: d.LastActivityExpr(),
		lead.SortCampaigns:      d.CampaignCountExpr(),
		lead.SortMemories:       d.MemoryCountExpr(),
		lead.SortLastMemoryAt:   d.LastMemoryAtExpr(),
		lead.SortRelatives:      "leads.relatives_count",
		lead.SortReferred:       "leads.referred_count",
	}
}

func orderBy(d infracrmfilter.LeadDescriptor, sorts []shared.Sort) string {
	exprs := sortExpressions(d)
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

type leadListRow struct {
	ID                string
	WorkspaceID       string
	Number            schema.OptionalText
	Name              string
	ProfilePictureURL string
	Age               *int
	Version           int64
	Blocked           bool
	BlockedAt         *time.Time
	BlockedBy         *string
	OwnerID           schema.OptionalText
	OwnerKind         schema.OptionalText
	CustomFields      datatypes.JSON
	RelativesCount    int
	ReferredCount     int
	CreatedAt         time.Time
	UpdatedAt         time.Time

	CampaignCount   int
	MemoryCount     int
	LastActivityAt  *time.Time
	LastMemoryAt    *time.Time
	WindowOpen      bool
	WindowExpiresAt *time.Time
}

func (row *leadListRow) toDomain() (*lead.Lead, error) {
	customFields, err := customFieldsOf(row.ID, row.CustomFields)
	if err != nil {
		return nil, err
	}
	l := &lead.Lead{
		ID:                row.ID,
		WorkspaceID:       row.WorkspaceID,
		Number:            string(row.Number),
		Name:              row.Name,
		ProfilePictureURL: row.ProfilePictureURL,
		StoredAge:         row.Age,
		Version:           row.Version,
		Blocked:           row.Blocked,
		BlockedBy:         row.BlockedBy,
		Owner:             ownerOf(row.OwnerID, row.OwnerKind),
		CustomFields:      customFields,
		RelativesCount:    row.RelativesCount,
		ReferredCount:     row.ReferredCount,
		CreatedAt:         row.CreatedAt,
		UpdatedAt:         row.UpdatedAt,
	}
	if row.BlockedAt != nil {
		l.BlockedAt = *row.BlockedAt
	}
	return l, nil
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
	if row.WindowOpen {
		summary.WindowExpiresAt = row.WindowExpiresAt
	}
	return summary
}

func (q *listQuery) pageColumns() string {
	return "leads.id, leads.workspace_id, leads.number, leads.name, leads.profile_picture_url, " +
		"leads.age, leads.blocked, leads.blocked_at, leads.blocked_by, leads.owner_id, leads.owner_kind, leads.custom_fields, " +
		"leads.relatives_count, leads.referred_count, leads.version, leads.created_at, leads.updated_at, " +
		"summary.campaign_count, summary.memory_count, summary.last_activity_at, summary.last_memory_at, " +
		"summary.window_open, summary.window_expires_at"
}

func (q *listQuery) summaryColumns() string {
	d := q.desc
	return d.CampaignCountExpr() + " AS campaign_count, " +
		d.MemoryCountExpr() + " AS memory_count, " +
		d.LastActivityExpr() + " AS last_activity_at, " +
		d.LastMemoryAtExpr() + " AS last_memory_at, " +
		d.WindowOpenExpr() + " AS window_open, " +
		d.WindowExpiresAtExpr() + " AS window_expires_at"
}

func (q *listQuery) pageIDs(opts shared.QueryOptions) (string, []interface{}) {
	pagination := shared.NormalizePaginationWithin(opts.Pagination, lead.MaxListPageSize)
	order, orderArgs := q.order(opts.Sorts)
	sql := "SELECT leads.id FROM leads WHERE " + q.where +
		" ORDER BY " + order +
		" LIMIT ? OFFSET ?"
	args := append(append([]interface{}{}, q.args...), orderArgs...)
	return sql, append(args, pagination.PageSize, opts.Pagination.OffsetWithin(lead.MaxListPageSize))
}

func (q *listQuery) order(sorts []shared.Sort) (string, []interface{}) {
	if len(sorts) == 0 && q.rank != "" {
		return q.rank + ", " + orderBy(q.desc, nil), q.rankArgs
	}
	return orderBy(q.desc, sorts), nil
}

func (q *listQuery) pageSummaries(ids []string) (string, []interface{}) {
	sql := "SELECT " + q.pageColumns() +
		" FROM leads LEFT JOIN LATERAL (SELECT " + q.summaryColumns() + ") summary ON true" +
		" WHERE leads.workspace_id = ? AND leads.id = ANY(?::uuid[])"
	return sql, []interface{}{q.desc.WorkspaceID, pq.StringArray(ids)}
}

func (r *repository) readFiltered(q *listQuery, read func(tx *gorm.DB) error) error {
	if !q.areas {
		return read(r.db)
	}
	return inReadSession(context.Background(), r.db, q.sessionSettings(mapSessionSettings), read)
}

func (r *repository) countLeads(workspaceID string, q *listQuery) (int64, error) {
	if cached, ok := r.agg.getCount(workspaceID, q); ok {
		return cached, nil
	}

	var total int64
	if err := r.readFiltered(q, func(tx *gorm.DB) error {
		return tx.Raw("SELECT COUNT(*) FROM leads WHERE "+q.where, q.args...).Scan(&total).Error
	}); err != nil {
		return 0, err
	}

	r.agg.setCount(workspaceID, q, total)
	return total, nil
}

func (r *repository) fetchPage(q *listQuery, opts shared.QueryOptions) ([]leadListRow, error) {
	idSQL, idArgs := q.pageIDs(opts)
	var ids []string
	if err := r.readFiltered(q, func(tx *gorm.DB) error { return tx.Raw(idSQL, idArgs...).Scan(&ids).Error }); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}

	summarySQL, summaryArgs := q.pageSummaries(ids)
	var rows []leadListRow
	if err := r.db.Raw(summarySQL, summaryArgs...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	return inPageOrder(ids, rows), nil
}

func inPageOrder(ids []string, rows []leadListRow) []leadListRow {
	byID := make(map[string]leadListRow, len(rows))
	for _, row := range rows {
		byID[row.ID] = row
	}
	ordered := make([]leadListRow, 0, len(ids))
	for _, id := range ids {
		if row, ok := byID[id]; ok {
			ordered = append(ordered, row)
		}
	}
	return ordered
}

func (r *repository) List(input lead.ListLeadsInput) (*shared.PaginatedResult[*lead.Lead], error) {
	q, err := r.compile(input)
	if err != nil {
		return nil, err
	}

	total, err := r.countLeads(input.WorkspaceID, q)
	if err != nil {
		return nil, err
	}

	rows, err := r.fetchPage(q, input.Options)
	if err != nil {
		return nil, err
	}

	items := make([]*lead.Lead, len(rows))
	for i := range rows {
		if items[i], err = rows[i].toDomain(); err != nil {
			return nil, err
		}
	}

	return shared.NewPaginatedResultWithin(items, input.Options.Pagination, total, lead.MaxListPageSize), nil
}

func (r *repository) ListWithSummary(input lead.ListLeadsInput) (*shared.PaginatedResult[*lead.LeadWithSummary], error) {
	q, err := r.compile(input)
	if err != nil {
		return nil, err
	}

	total, err := r.countLeads(input.WorkspaceID, q)
	if err != nil {
		return nil, err
	}

	rows, err := r.fetchPage(q, input.Options)
	if err != nil {
		return nil, err
	}

	items := make([]*lead.LeadWithSummary, len(rows))
	for i := range rows {
		l, err := rows[i].toDomain()
		if err != nil {
			return nil, err
		}
		items[i] = &lead.LeadWithSummary{Lead: l, Summary: rows[i].toSummary()}
	}

	return shared.NewPaginatedResultWithin(items, input.Options.Pagination, total, lead.MaxListPageSize), nil
}

type facetRow struct {
	Key   string
	Count int64
}

func countsOf(rows []facetRow) map[string]int64 {
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

	if cached, ok := r.agg.getFacets(input.WorkspaceID, q); ok {
		return cached, nil
	}

	d := q.desc

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
		" COUNT(*) FILTER (WHERE " + d.HasCampaignExpr() + ") AS with_campaign," +
		" COUNT(*) FILTER (WHERE " + d.HasMemoryExpr() + ") AS with_memory," +
		" COUNT(*) FILTER (WHERE NULLIF(leads.name, '') IS NOT NULL) AS named" +
		" FROM leads WHERE " + q.where
	if err := r.readFiltered(q, func(tx *gorm.DB) error { return tx.Raw(sql, q.args...).Scan(&agg).Error }); err != nil {
		return nil, err
	}

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

	err = r.inSection(context.Background(), q, func(tx *gorm.DB) error {
		var err error
		facets.CampaignStatuses, facets.Channels, facets.MemoryCategories, err = readDistinctFacets(tx, q)
		return err
	})
	if err != nil {
		return nil, err
	}

	r.agg.setFacets(input.WorkspaceID, q, facets)
	r.agg.setCount(input.WorkspaceID, q, facets.Total)

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

func (r *repository) AttachContactDetails(ctx context.Context, workspaceID string, leads []*lead.Lead) error {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return lead.ErrLeadWorkspaceRequired
	}
	return attachCollections(r.db.WithContext(ctx), workspaceID, leads, recordAggregate)
}
