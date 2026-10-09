package lead

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"vozko/domain/cache"
	"vozko/domain/lead"
	"vozko/domain/leadaction"
	"vozko/infra/database"
	"vozko/infra/database/schema"
)

const lookupBatchSize = 500

type repository struct {
	db    *gorm.DB
	agg   *aggregateCache
	newID func() string
}

type Repository interface {
	lead.Repository
	lead.Store
	lead.EntryDirectory
	lead.EntryUsage
	lead.RelationStore
	lead.EntryLeads
	lead.ContactDetails
	lead.DuplicateFinder
	lead.Anonymizer
	lead.SectionReader
	lead.TimelineSource
	lead.SelectionReader
	lead.SelectionSnapshots
	leadaction.BulkWriter
}

func NewRepository(db *gorm.DB) Repository {
	return &repository{db: db, agg: newAggregateCache(nil)}
}

func NewCachedRepository(db *gorm.DB, state cache.SharedState) Repository {
	return &repository{db: db, agg: newAggregateCache(state)}
}

func (r *repository) scope(workspaceID string) *gorm.DB {
	return r.db.Where("workspace_id = ?", workspaceID)
}

func (r *repository) leadID() string {
	if r.newID != nil {
		return r.newID()
	}
	return uuid.NewString()
}

func onLiveIdentityConflictDoNothing() clause.OnConflict {
	return clause.OnConflict{
		Columns:     []clause.Column{{Name: "workspace_id"}, {Name: "number"}},
		TargetWhere: clause.Where{Exprs: []clause.Expression{clause.Expr{SQL: "deleted_at IS NULL"}}},
		DoNothing:   true,
	}
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
	if len(database.UUIDArray([]string{id})) == 0 {
		return nil, lead.ErrLeadNotFound
	}

	var row schema.Lead
	if err := r.scope(workspaceID).Where("id = ?", id).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, lead.ErrLeadNotFound
		}
		return nil, err
	}
	return toDomain(&row)
}

func (r *repository) FindByNumber(workspaceID, number string) (*lead.Lead, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, lead.ErrLeadWorkspaceRequired
	}
	formats := lead.NumberFormats(number)
	if len(formats) == 0 {
		return nil, lead.ErrLeadInvalid
	}
	return r.findByFormats(workspaceID, formats)
}

func (r *repository) findByFormats(workspaceID string, formats []string) (*lead.Lead, error) {
	var row schema.Lead
	if err := r.scope(workspaceID).Where("number IN ?", formats).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, lead.ErrLeadNotFound
		}
		return nil, err
	}
	return toDomain(&row)
}

func (r *repository) FindByNumbers(workspaceID string, numbers []string) ([]*lead.Lead, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, lead.ErrLeadWorkspaceRequired
	}
	formats := numberFormats(numbers)
	if len(formats) == 0 {
		return nil, nil
	}
	rows, err := r.numberHolders(r.db, workspaceID, formats, directoryLookupLimit)
	if err != nil {
		return nil, err
	}
	leads, err := toDomainAll(rows)
	if err != nil {
		return nil, err
	}
	if err := attachCollections(r.db, workspaceID, leads, phonesOnly); err != nil {
		return nil, err
	}
	return leads, nil
}

func (r *repository) FindByIDs(workspaceID string, ids []string) ([]*lead.Lead, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, lead.ErrLeadWorkspaceRequired
	}
	valid := database.UUIDArray(ids)
	if len(valid) == 0 {
		return []*lead.Lead{}, nil
	}

	var rows []schema.Lead
	if err := r.scope(workspaceID).Where("id = ANY(?::uuid[])", valid).Find(&rows).Error; err != nil {
		return nil, err
	}
	return toDomainAll(rows)
}

func (r *repository) FindOrCreate(workspaceID, number string, update lead.LeadUpdate) (*lead.Lead, bool, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, false, lead.ErrLeadWorkspaceRequired
	}
	if !update.Source.Valid() {
		return nil, false, lead.ErrLeadSourceInvalid
	}
	normalized := lead.NormalizeNumber(number)
	if normalized == "" {
		return nil, false, lead.ErrLeadInvalid
	}
	formats := lead.NumberFormats(normalized)

	existing, err := r.findByFormats(workspaceID, formats)
	if errors.Is(err, lead.ErrLeadNotFound) {
		existing, err = r.promoteIdentity(workspaceID, normalized, pq.StringArray(formats))
		if err != nil {
			return nil, false, err
		}
	}
	if existing == nil && err == nil {
		created, inserted, insertErr := r.insertIdentity(workspaceID, normalized, update)
		if insertErr != nil {
			return nil, false, insertErr
		}
		if inserted {
			r.agg.bump(workspaceID)
			return created, true, nil
		}
		existing, err = r.findByFormats(workspaceID, formats)
	}
	if err != nil {
		return nil, false, err
	}

	wrote, err := r.mergeIncoming(workspaceID, []incomingMerge{{lead: existing, apply: mergeOf(update)}})
	if err != nil {
		return nil, false, err
	}
	if wrote {
		r.agg.bump(workspaceID)
	}
	return existing, false, nil
}

func (r *repository) newIdentityLead(workspaceID, number string, update lead.LeadUpdate) *lead.Lead {
	l := &lead.Lead{
		ID:          r.leadID(),
		WorkspaceID: workspaceID,
		Number:      number,
		Source:      update.Source,
		Version:     1,
	}
	l.MergeIncoming(update)
	return l
}

func (r *repository) insertIdentity(workspaceID, number string, update lead.LeadUpdate) (*lead.Lead, bool, error) {
	row, err := toSchema(r.newIdentityLead(workspaceID, number, update))
	if err != nil {
		return nil, false, err
	}
	res := r.db.Clauses(onLiveIdentityConflictDoNothing()).Create(row)
	if res.Error != nil {
		return nil, false, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, false, nil
	}
	created, err := toDomain(row)
	if err != nil {
		return nil, false, err
	}
	return created, true, nil
}

type incomingBatch struct {
	numbers []string
	inputs  map[string]lead.BulkLeadInput
}

func prepareIncoming(inputs []lead.BulkLeadInput, source func(lead.BulkLeadInput) lead.Source) (incomingBatch, error) {
	batch := incomingBatch{inputs: make(map[string]lead.BulkLeadInput, len(inputs))}
	for _, input := range inputs {
		input.Source = source(input)
		if !input.Source.Valid() {
			return incomingBatch{}, lead.ErrLeadSourceInvalid
		}
		normalized := lead.NormalizeNumber(input.Number)
		if normalized == "" {
			continue
		}
		if _, exists := batch.inputs[normalized]; !exists {
			batch.numbers = append(batch.numbers, normalized)
			batch.inputs[normalized] = input
		}
	}
	return batch, nil
}

func (b incomingBatch) update(number string) lead.LeadUpdate {
	input := b.inputs[number]
	return lead.LeadUpdate{Source: input.Source, Name: input.Name}
}

func (r *repository) existingByNumber(workspaceID string, numbers []string) (map[string]*lead.Lead, error) {
	search := make([]string, 0, len(numbers)*2)
	for _, number := range numbers {
		search = append(search, lead.NumberFormats(number)...)
	}

	found := make(map[string]*lead.Lead, len(search))
	for start := 0; start < len(search); start += lookupBatchSize {
		end := min(start+lookupBatchSize, len(search))
		var rows []schema.Lead
		if err := r.scope(workspaceID).Where("number IN ?", search[start:end]).Find(&rows).Error; err != nil {
			return nil, err
		}
		leads, err := toDomainAll(rows)
		if err != nil {
			return nil, err
		}
		for _, l := range leads {
			for _, format := range lead.NumberFormats(l.Number) {
				if _, mapped := found[format]; !mapped || format == l.Number {
					found[format] = l
				}
			}
		}
	}
	return found, nil
}

type insertedIdentities struct {
	created map[string]*lead.Lead
	raced   map[string]*lead.Lead
}

func (r *repository) insertMissing(workspaceID string, batch incomingBatch, missing []string) (insertedIdentities, error) {
	out := insertedIdentities{created: make(map[string]*lead.Lead, len(missing)), raced: map[string]*lead.Lead{}}
	rows := make([]schema.Lead, 0, len(missing))
	generated := make(map[string]string, len(missing))
	for _, number := range missing {
		row, err := toSchema(r.newIdentityLead(workspaceID, number, batch.update(number)))
		if err != nil {
			return insertedIdentities{}, err
		}
		generated[number] = row.ID
		rows = append(rows, *row)
	}
	res := r.db.Clauses(onLiveIdentityConflictDoNothing()).CreateInBatches(&rows, lookupBatchSize)
	if res.Error != nil {
		return insertedIdentities{}, res.Error
	}

	if res.RowsAffected == int64(len(rows)) {
		created, err := toDomainAll(rows)
		if err != nil {
			return insertedIdentities{}, err
		}
		for _, l := range created {
			out.created[l.Number] = l
		}
		return out, nil
	}

	stored, err := r.existingByNumber(workspaceID, missing)
	if err != nil {
		return insertedIdentities{}, err
	}
	for _, number := range missing {
		l, ok := stored[number]
		switch {
		case !ok:
		case l.ID == generated[number]:
			out.created[number] = l
		default:
			out.raced[number] = l
		}
	}
	return out, nil
}

type incomingOutcome struct {
	leads   map[string]*lead.Lead
	matched []*lead.Lead
	created int
}

func (r *repository) resolveIncoming(workspaceID string, batch incomingBatch, applyFor func(number string) func(*lead.Lead) []string) (incomingOutcome, error) {
	out := incomingOutcome{leads: make(map[string]*lead.Lead, len(batch.numbers))}
	if len(batch.numbers) == 0 {
		return out, nil
	}
	existing, err := r.existingByNumber(workspaceID, batch.numbers)
	if err != nil {
		return incomingOutcome{}, err
	}

	var merges []incomingMerge
	var missing []string
	matched := func(number string, l *lead.Lead) {
		out.leads[number] = l
		out.matched = append(out.matched, l)
		merges = append(merges, incomingMerge{lead: l, apply: applyFor(number)})
	}
	for _, number := range batch.numbers {
		if found, ok := existing[number]; ok {
			matched(number, found)
			continue
		}
		missing = append(missing, number)
	}

	if len(missing) > 0 {
		inserted, err := r.insertMissing(workspaceID, batch, missing)
		if err != nil {
			return incomingOutcome{}, err
		}
		for number, l := range inserted.created {
			out.leads[number] = l
		}
		out.created = len(inserted.created)
		for _, number := range missing {
			if l, ok := inserted.raced[number]; ok {
				matched(number, l)
			}
		}
	}

	wrote, err := r.mergeIncoming(workspaceID, merges)
	if err != nil {
		return incomingOutcome{}, err
	}
	if wrote || out.created > 0 {
		r.agg.bump(workspaceID)
	}
	return out, nil
}

func (r *repository) FindOrCreateMany(workspaceID string, inputs []lead.BulkLeadInput) (map[string]*lead.Lead, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, lead.ErrLeadWorkspaceRequired
	}
	batch, err := prepareIncoming(inputs, func(in lead.BulkLeadInput) lead.Source { return in.Source })
	if err != nil {
		return nil, err
	}
	out, err := r.resolveIncoming(workspaceID, batch, func(number string) func(*lead.Lead) []string {
		return mergeOf(batch.update(number))
	})
	if err != nil {
		return nil, err
	}
	return out.leads, nil
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
	if err := r.db.Exec("UPDATE leads SET deleted_at = ?, version = version + 1 WHERE id = ? AND workspace_id = ? AND deleted_at IS NULL",
		time.Now().UTC(), id, workspaceID).Error; err != nil {
		return err
	}
	r.agg.bump(workspaceID)
	return nil
}
