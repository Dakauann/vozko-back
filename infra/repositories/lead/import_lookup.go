package lead

import (
	"context"
	"errors"
	"strings"

	"vozko/domain/lead"
	"vozko/domain/leadimport"
	"vozko/infra/database/schema"
)

const (
	importIdentitySQL = "SELECT * FROM leads WHERE workspace_id = ? AND number = ANY(?) AND deleted_at IS NULL"

	importHoldersSQL = "SELECT * FROM leads WHERE leads.workspace_id = ? AND leads.deleted_at IS NULL AND leads.id IN (" + numberHoldersSQL + ")" +
		" ORDER BY leads.id LIMIT ?"

	importIdentityIDsSQL = "SELECT id::text AS id, number FROM leads WHERE workspace_id = ? AND number = ANY(?) AND deleted_at IS NULL"

	importHolderCountsSQL = "SELECT formats.number, (SELECT count(*) FROM (" +
		"SELECT leads.id FROM leads WHERE leads.workspace_id = ? AND leads.number = formats.number AND leads.deleted_at IS NULL" +
		" UNION ALL SELECT lead_phones.lead_id FROM lead_phones JOIN leads ON leads.id = lead_phones.lead_id AND leads.deleted_at IS NULL" +
		" WHERE lead_phones.workspace_id = ? AND lead_phones.number = formats.number LIMIT ?) AS capped) AS holders" +
		" FROM unnest(CAST(? AS text[])) AS formats(number)"
)

var errNotTheLeadRepository = errors.New("lead imports: the lead repository of this package is required")

type Imports struct {
	r *repository
}

var (
	_ leadimport.Store  = (*Imports)(nil)
	_ leadimport.Writer = (*Imports)(nil)
	_ leadimport.Lookup = (*Imports)(nil)
)

func NewImports(leads Repository) (*Imports, error) {
	impl, ok := leads.(*repository)
	if !ok || impl == nil {
		return nil, errNotTheLeadRepository
	}
	return &Imports{r: impl}, nil
}

func (s *Imports) ByIdentity(ctx context.Context, workspaceID string, numbers []string) (map[string]*lead.Lead, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, lead.ErrLeadWorkspaceRequired
	}
	formats := numberFormats(numbers)
	out := make(map[string]*lead.Lead, len(formats))
	if len(formats) == 0 {
		return out, nil
	}
	db := s.r.db.WithContext(ctx)
	var rows []schema.Lead
	if err := db.Raw(importIdentitySQL, workspaceID, formats).Scan(&rows).Error; err != nil {
		return nil, err
	}
	leads, err := toDomainAll(rows)
	if err != nil {
		return nil, err
	}
	if err := attachCollections(db, workspaceID, leads, recordAggregate); err != nil {
		return nil, err
	}
	for _, l := range leads {
		lead.IndexByNumber(out, l.Number, l)
	}
	return out, nil
}

func (s *Imports) HoldingPhones(ctx context.Context, workspaceID string, numbers []string) ([]*lead.Lead, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, lead.ErrLeadWorkspaceRequired
	}
	formats := numberFormats(numbers)
	if len(formats) == 0 {
		return nil, nil
	}
	db := s.r.db.WithContext(ctx)
	var rows []schema.Lead
	if err := db.Raw(importHoldersSQL, workspaceID, workspaceID, formats, workspaceID, formats, leadimport.MaxContactHolders+1).Scan(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) > leadimport.MaxContactHolders {
		return nil, leadimport.ErrTooManyHolders
	}
	leads, err := toDomainAll(rows)
	if err != nil {
		return nil, err
	}
	if err := attachCollections(db, workspaceID, leads, recordAggregate); err != nil {
		return nil, err
	}
	return leads, nil
}

type holderCountRow struct {
	Number  string
	Holders int
}

func (s *Imports) PhoneHolderCounts(ctx context.Context, workspaceID string, numbers []string) (map[string]int, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, lead.ErrLeadWorkspaceRequired
	}
	formats := numberFormats(numbers)
	counts := make(map[string]int, len(formats))
	if len(formats) == 0 {
		return counts, nil
	}
	var rows []holderCountRow
	if err := s.r.db.WithContext(ctx).Raw(importHolderCountsSQL, workspaceID, workspaceID, leadimport.MaxContactHolders+1, formats).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		counts[row.Number] = row.Holders
	}
	return counts, nil
}

type identityRow struct {
	ID     string
	Number string
}

func (s *Imports) IdentityIDs(ctx context.Context, workspaceID string, numbers []string) (map[string]string, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, lead.ErrLeadWorkspaceRequired
	}
	formats := numberFormats(numbers)
	out := make(map[string]string, len(formats))
	if len(formats) == 0 {
		return out, nil
	}
	var rows []identityRow
	if err := s.r.db.WithContext(ctx).Raw(importIdentityIDsSQL, workspaceID, formats).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		lead.IndexByNumber(out, row.Number, row.ID)
	}
	return out, nil
}
