package lead

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"vozko/domain/lead"
	"vozko/domain/leadimport"
	"vozko/domain/recordevent"
	"vozko/infra/database/schema"
)

const (
	importWriteChunk = 500

	importLockSQL = "SELECT id::text AS id, version" + lockLeadsFromSQL

	importReloadSQL = "SELECT * FROM leads WHERE workspace_id = ? AND id = ANY(?::uuid[]) AND deleted_at IS NULL"

	importInsertedSQL = "SELECT id::text FROM leads WHERE workspace_id = ? AND id = ANY(?::uuid[])"

	importEnrichSQL = "UPDATE leads SET name = v.name, name_source = NULLIF(v.name_source, ''), nickname = NULLIF(v.nickname, '')," +
		" email = NULLIF(v.email, ''), birth_date = NULLIF(v.birth_date, '')::date, owner_id = NULLIF(v.owner_id, '')::uuid," +
		" owner_kind = NULLIF(v.owner_kind, ''), custom_fields = NULLIF(v.custom_fields, '')::jsonb," +
		" whatsapp_opt_in_at = NULLIF(v.opt_in_at, '')::timestamptz, whatsapp_opt_in_source = NULLIF(v.opt_in_source, '')," +
		" whatsapp_opt_in_purpose = NULLIF(v.opt_in_purpose, ''), updated_at = ?, version = leads.version + 1" +
		" FROM unnest(?::uuid[], ?::bigint[], ?::text[], ?::text[], ?::text[], ?::text[], ?::text[], ?::text[], ?::text[], ?::text[], ?::text[], ?::text[], ?::text[])" +
		" AS v(id, version, name, name_source, nickname, email, birth_date, owner_id, owner_kind, custom_fields, opt_in_at, opt_in_source, opt_in_purpose)" +
		" WHERE leads.id = v.id AND leads.version = v.version AND leads.workspace_id = ? AND leads.deleted_at IS NULL" +
		" RETURNING leads.id::text AS id, leads.version AS version"

	importCheckpointSQL = "UPDATE lead_imports SET processed = ?, result = ?::jsonb, heartbeat_at = ?, updated_at = ?" +
		" WHERE id = ? AND claim_token = ? AND status = 'importing' RETURNING id"

	importPendingLinksSQL = "SELECT id::text AS id, line, lead_id::text AS lead_id, relative_number, kind FROM lead_import_links" +
		" WHERE import_id = ? ORDER BY lead_import_links.line, lead_import_links.id LIMIT ?"

	importStampFilledSQL = "UPDATE lead_addresses SET import_id = ? WHERE workspace_id = ? AND id = ANY(?::uuid[])"

	importDeleteLinksSQL = "DELETE FROM lead_import_links WHERE import_id = ? AND id = ANY(?::uuid[])"

	importInsertRelationsSQL = "INSERT INTO lead_relations (id, workspace_id, lead_id, other_lead_id, dimension, kind, created_by, created_at)" +
		" SELECT u.id, ?::uuid, u.lead_id, u.other_lead_id, u.dimension, u.kind, NULLIF(u.created_by, '')::uuid, ?" +
		" FROM unnest(?::uuid[], ?::uuid[], ?::uuid[], ?::text[], ?::text[], ?::text[]) AS u(id, lead_id, other_lead_id, dimension, kind, created_by)" +
		" ON CONFLICT DO NOTHING RETURNING id::text"
)

type importItem struct {
	outcome  leadimport.RowOutcome
	existing *lead.Lead
}

func (s *Imports) WriteRows(ctx context.Context, batch leadimport.RowBatch) error {
	if batch.Job == nil || batch.Decide == nil || batch.Checkpoint == nil {
		return fmt.Errorf("lead import: a row batch needs its job, its decision and its checkpoint")
	}
	ws := batch.Job.WorkspaceID
	wrote := false
	err := s.r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now().UTC()
		items := make([]*importItem, 0, len(batch.Items))
		for _, in := range batch.Items {
			it := &importItem{existing: in.Existing, outcome: leadimport.RowOutcome{Record: in.Record, Matched: in.Existing}}
			if err := it.decide(batch.Decide); err != nil {
				return err
			}
			items = append(items, it)
		}
		created, err := s.insertCreated(tx, ws, items, now, batch.Decide)
		if err != nil {
			return err
		}
		enriched, err := s.writeEnriched(tx, ws, items, now, batch.Decide)
		if err != nil {
			return err
		}
		if err := writeAdditions(tx, ws, batch.Job.ID, append(created, enriched...), now, s.r.leadID); err != nil {
			return err
		}
		if err := writeImportEvents(tx, ws, batch, append(created, enriched...), now); err != nil {
			return err
		}
		wrote = len(created)+len(enriched) > 0
		outcomes := make([]leadimport.RowOutcome, len(items))
		for i, it := range items {
			outcomes[i] = it.outcome
		}
		return s.checkpointRows(tx, batch.Job, batch.Checkpoint(outcomes), now)
	})
	if err != nil {
		return err
	}
	if wrote {
		s.r.agg.bump(ws)
	}
	return nil
}

func (it *importItem) decide(decide leadimport.Decide) error {
	d, err := decide(it.existing, it.outcome.Record)
	if err != nil {
		return err
	}
	it.outcome.Decision, it.outcome.Matched = d, it.existing
	if it.existing != nil {
		it.outcome.LeadID = it.existing.ID
	}
	return nil
}

func (it *importItem) reject(reason lead.RejectReason) {
	it.outcome.Decision = lead.ImportDecision{Verdict: lead.ImportRejected,
		Issues: []lead.ImportIssue{{Line: it.outcome.Record.Line, Reason: reason, Rejected: true}}}
	it.outcome.LeadID = ""
}

func (s *Imports) insertCreated(tx *gorm.DB, ws string, items []*importItem, now time.Time, decide leadimport.Decide) ([]*importItem, error) {
	var creating []*importItem
	var rows []schema.Lead
	for _, it := range items {
		if it.outcome.Decision.Verdict != lead.ImportCreated {
			continue
		}
		l := it.outcome.Decision.Lead
		l.ID, l.Version, l.CreatedAt, l.UpdatedAt = s.r.leadID(), 1, now, now
		row, err := toSchema(l)
		if err != nil {
			return nil, err
		}
		creating = append(creating, it)
		rows = append(rows, *row)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	res := tx.Clauses(onLiveIdentityConflictDoNothing()).CreateInBatches(&rows, importWriteChunk)
	if res.Error != nil {
		return nil, res.Error
	}
	inserted := map[string]bool{}
	if res.RowsAffected == int64(len(rows)) {
		for _, row := range rows {
			inserted[row.ID] = true
		}
	} else {
		ids := make(pq.StringArray, len(rows))
		for i, row := range rows {
			ids[i] = row.ID
		}
		var found []string
		if err := tx.Raw(importInsertedSQL, ws, ids).Scan(&found).Error; err != nil {
			return nil, err
		}
		for _, id := range found {
			inserted[id] = true
		}
	}
	var created, raced []*importItem
	for _, it := range creating {
		if inserted[it.outcome.Decision.Lead.ID] {
			it.outcome.LeadID = it.outcome.Decision.Lead.ID
			created = append(created, it)
			continue
		}
		raced = append(raced, it)
	}
	if err := s.adoptWinners(tx, ws, raced, decide); err != nil {
		return nil, err
	}
	return created, nil
}

func (s *Imports) adoptWinners(tx *gorm.DB, ws string, raced []*importItem, decide leadimport.Decide) error {
	if len(raced) == 0 {
		return nil
	}
	numbers := make([]string, len(raced))
	for i, it := range raced {
		numbers[i] = it.outcome.Record.Number
	}
	var rows []schema.Lead
	if err := tx.Raw(importIdentitySQL, ws, numberFormats(numbers)).Scan(&rows).Error; err != nil {
		return err
	}
	winners, err := toDomainAll(rows)
	if err != nil {
		return err
	}
	if err := attachCollections(tx, ws, winners, recordAggregate); err != nil {
		return err
	}
	for _, it := range raced {
		var winner *lead.Lead
		for _, w := range winners {
			if w.HoldsIdentity(it.outcome.Record.Number) {
				winner = w
			}
		}
		if winner == nil {
			it.reject(lead.ReasonLeadChanging)
			continue
		}
		it.existing = winner
		if err := it.decide(decide); err != nil {
			return err
		}
	}
	return nil
}

type lockedVersion struct {
	ID      string
	Version int64
}

func (s *Imports) writeEnriched(tx *gorm.DB, ws string, items []*importItem, now time.Time, decide leadimport.Decide) ([]*importItem, error) {
	byID := map[string]*importItem{}
	var ids []string
	for _, it := range items {
		if it.outcome.Decision.Verdict == lead.ImportEnriched {
			byID[it.existing.ID] = it
			ids = append(ids, it.existing.ID)
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	sort.Strings(ids)
	var locked []lockedVersion
	if err := tx.Raw(importLockSQL, ws, pq.StringArray(ids)).Scan(&locked).Error; err != nil {
		return nil, err
	}
	current := map[string]int64{}
	for _, l := range locked {
		current[l.ID] = l.Version
	}
	var stale []string
	for _, id := range ids {
		version, alive := current[id]
		switch {
		case !alive:
			byID[id].reject(lead.ReasonLeadChanging)
		case version != byID[id].existing.Version:
			stale = append(stale, id)
		}
	}
	if err := s.redecide(tx, ws, byID, stale, decide); err != nil {
		return nil, err
	}
	var writing []*importItem
	for _, id := range ids {
		if it := byID[id]; it.outcome.Decision.Verdict == lead.ImportEnriched {
			writing = append(writing, it)
		}
	}
	return writing, s.updateEnriched(tx, ws, writing, now)
}

func (s *Imports) redecide(tx *gorm.DB, ws string, byID map[string]*importItem, stale []string, decide leadimport.Decide) error {
	if len(stale) == 0 {
		return nil
	}
	var rows []schema.Lead
	if err := tx.Raw(importReloadSQL, ws, pq.StringArray(stale)).Scan(&rows).Error; err != nil {
		return err
	}
	fresh, err := toDomainAll(rows)
	if err != nil {
		return err
	}
	if err := attachCollections(tx, ws, fresh, recordAggregate); err != nil {
		return err
	}
	for _, l := range fresh {
		it := byID[l.ID]
		it.existing = l
		if err := it.decide(decide); err != nil {
			return err
		}
	}
	return nil
}

func nullText(value string) sql.NullString {
	return sql.NullString{String: value, Valid: value != ""}
}

func (s *Imports) updateEnriched(tx *gorm.DB, ws string, items []*importItem, now time.Time) error {
	if len(items) == 0 {
		return nil
	}
	n := len(items)
	ids := make(pq.StringArray, n)
	versions := make(pq.Int64Array, n)
	names := make(pq.StringArray, n)
	texts := make([][]sql.NullString, 10)
	for i := range texts {
		texts[i] = make([]sql.NullString, n)
	}
	for i, it := range items {
		l := it.outcome.Decision.Lead
		customFields, err := customFieldsColumn(l.CustomFields)
		if err != nil {
			return err
		}
		ownerID, ownerKind := ownerColumns(l.Owner)
		optInAt, optInSource, optInPurpose := consentColumns(l.WhatsAppOptIn)
		at := ""
		if optInAt != nil {
			at = optInAt.UTC().Format(time.RFC3339Nano)
		}
		ids[i], versions[i], names[i] = l.ID, it.existing.Version, l.Name
		for col, value := range []string{string(l.NameSource), l.Nickname, l.Email, string(birthDateColumn(l.BirthDate)), string(ownerID),
			string(ownerKind), string(customFields), at, string(optInSource), string(optInPurpose)} {
			texts[col][i] = nullText(value)
		}
	}
	args := []interface{}{now, ids, versions, names}
	for _, column := range texts {
		args = append(args, pq.GenericArray{A: column})
	}
	args = append(args, ws)
	var written []lockedVersion
	if err := tx.Raw(importEnrichSQL, args...).Scan(&written).Error; err != nil {
		return err
	}
	versionsOf := map[string]int64{}
	for _, w := range written {
		versionsOf[w.ID] = w.Version
	}
	for _, it := range items {
		version, ok := versionsOf[it.outcome.LeadID]
		if !ok {
			return fmt.Errorf("lead import: lead %s changed while it was locked", it.outcome.LeadID)
		}
		it.outcome.Decision.Lead.Version, it.outcome.Decision.Lead.UpdatedAt = version, now
	}
	return nil
}

func writeAdditions(tx *gorm.DB, ws, importID string, items []*importItem, now time.Time, newID func() string) error {
	var phones []schema.LeadPhone
	var addresses []schema.LeadAddress
	for _, it := range items {
		d := it.outcome.Decision
		l := d.Lead
		newPhones, newAddresses, offset := d.NewPhones, []lead.Address{}, len(l.Phones)-len(d.NewPhones)
		if d.NewAddress != nil {
			newAddresses = append(newAddresses, *d.NewAddress)
		}
		if d.Verdict == lead.ImportCreated {
			newPhones, newAddresses, offset = l.Phones, l.Addresses, 0
		}
		for i, p := range newPhones {
			phones = append(phones, schema.LeadPhone{ID: newID(), WorkspaceID: ws, LeadID: l.ID, Number: p.Number, Label: string(p.Label), Position: offset + i, CreatedAt: now})
		}
		for i, a := range newAddresses {
			a.ID = newID()
			row := addressRow(l, len(l.Addresses)-len(newAddresses)+i, a, now)
			row.ImportID = schema.OptionalText(importID)
			addresses = append(addresses, row)
		}
	}
	if len(phones) > 0 {
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(&phones, importWriteChunk).Error; err != nil {
			return err
		}
	}
	if len(addresses) > 0 {
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(&addresses, importWriteChunk).Error; err != nil {
			return err
		}
	}
	return writeFilledAddresses(tx, ws, importID, items, now)
}

func writeFilledAddresses(tx *gorm.DB, ws, importID string, items []*importItem, now time.Time) error {
	var filledIDs pq.StringArray
	for _, it := range items {
		filled := it.outcome.Decision.FilledAddress
		if filled == nil || it.existing == nil {
			continue
		}
		position := slices.IndexFunc(it.existing.Addresses, func(a lead.Address) bool { return a.ID == filled.ID })
		if position < 0 {
			return fmt.Errorf("lead import: address %s of lead %s is not stored", filled.ID, it.existing.ID)
		}
		previous := addressRow(it.existing, position, it.existing.Addresses[position], now)
		if err := updateAddress(tx, addressRow(it.outcome.Decision.Lead, position, *filled, now), previous); err != nil {
			return err
		}
		filledIDs = append(filledIDs, filled.ID)
	}
	if len(filledIDs) == 0 {
		return nil
	}
	return tx.Exec(importStampFilledSQL, importID, ws, filledIDs).Error
}

func writeImportEvents(tx *gorm.DB, ws string, batch leadimport.RowBatch, items []*importItem, now time.Time) error {
	var rows []schema.LeadEvent
	for _, it := range items {
		event := it.outcome.Decision.Event(batch.ActorID, it.existing, batch.Definitions)
		leadRows, err := eventRows(ws, it.outcome.LeadID, []recordevent.Event{event}, now)
		if err != nil {
			return err
		}
		rows = append(rows, leadRows...)
	}
	return insertEventRows(tx, rows)
}

func (s *Imports) checkpointRows(tx *gorm.DB, job *leadimport.Job, cp leadimport.Checkpoint, now time.Time) error {
	if err := insertIssues(tx, job.ID, cp.Issues); err != nil {
		return err
	}
	if len(cp.Links) > 0 {
		links := make([]schema.LeadImportLink, len(cp.Links))
		for i, l := range cp.Links {
			links[i] = schema.LeadImportLink{ID: uuid.NewString(), ImportID: job.ID, WorkspaceID: job.WorkspaceID, Line: l.Line,
				LeadID: l.LeadID, RelativeNumber: l.RelativeNumber, Kind: string(l.Kind)}
		}
		if err := tx.CreateInBatches(&links, importWriteChunk).Error; err != nil {
			return err
		}
	}
	return checkpoint(tx, job, cp.Processed, cp.Result, now)
}

func insertIssues(tx *gorm.DB, importID string, issues []lead.ImportIssue) error {
	if len(issues) == 0 {
		return nil
	}
	rows := make([]schema.LeadImportIssue, len(issues))
	for i, issue := range issues {
		rows[i] = schema.LeadImportIssue{ImportID: importID, Line: issue.Line, Reason: string(issue.Reason), Field: schema.OptionalText(issue.Field), Rejected: issue.Rejected}
	}
	return tx.CreateInBatches(&rows, importWriteChunk).Error
}

func checkpoint(tx *gorm.DB, job *leadimport.Job, processed int, result leadimport.Counts, now time.Time) error {
	raw, err := jsonColumn(result, true)
	if err != nil {
		return err
	}
	var ids []string
	if err := tx.Raw(importCheckpointSQL, processed, string(raw), now, now, job.ID, job.Claim).Scan(&ids).Error; err != nil {
		return err
	}
	if len(ids) == 0 {
		return leadimport.ErrClaimLost
	}
	return nil
}

type pendingLinkRow struct {
	ID             string
	Line           int
	LeadID         string
	RelativeNumber string
	Kind           string
}

func (s *Imports) PendingLinks(ctx context.Context, importID string, limit int) ([]leadimport.PendingLink, error) {
	var rows []pendingLinkRow
	if err := s.r.db.WithContext(ctx).Raw(importPendingLinksSQL, importID, max(1, limit)).Scan(&rows).Error; err != nil {
		return nil, err
	}
	links := make([]leadimport.PendingLink, len(rows))
	for i, row := range rows {
		links[i] = leadimport.PendingLink{ID: row.ID, Line: row.Line, LeadID: row.LeadID, RelativeNumber: row.RelativeNumber, Kind: lead.RelationKind(row.Kind)}
	}
	return links, nil
}

func (s *Imports) WriteLinks(ctx context.Context, batch leadimport.LinkBatch) error {
	if batch.Job == nil || batch.Checkpoint == nil {
		return fmt.Errorf("lead import: a link batch needs its job and its checkpoint")
	}
	ws := batch.Job.WorkspaceID
	wrote := false
	err := s.r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now().UTC()
		failed, candidates, done := splitLinks(batch.Links)
		present, err := lockedSides(tx, ws, candidates)
		if err != nil {
			return err
		}
		var relations []lead.Relation
		lines := map[string]int{}
		for _, c := range candidates {
			if !present[c.relation.LeadID] || !present[c.relation.OtherLeadID] {
				failed = append(failed, lead.ImportIssue{Line: c.line, Reason: lead.ReasonRelativeNotFound, Field: lead.ImportFieldRelative})
				continue
			}
			rel := c.relation
			rel.ID, rel.CreatedAt = s.r.leadID(), now
			lines[rel.ID] = c.line
			relations = append(relations, rel)
		}
		inserted, err := insertImportRelations(tx, ws, relations, now)
		if err != nil {
			return err
		}
		failed = append(failed, alreadyLinked(relations, inserted, lines)...)
		if _, err := applyRelationChange(tx, ws, relationChange{added: inserted}, "", now); err != nil {
			return err
		}
		wrote = len(inserted) > 0
		sort.SliceStable(failed, func(i, j int) bool { return failed[i].Line < failed[j].Line })
		result := batch.Checkpoint(len(inserted), failed)
		if err := insertIssues(tx, batch.Job.ID, failed); err != nil {
			return err
		}
		if err := tx.Exec(importDeleteLinksSQL, batch.Job.ID, done).Error; err != nil {
			return err
		}
		return checkpoint(tx, batch.Job, batch.Job.Processed, result, now)
	})
	if err != nil {
		return err
	}
	if wrote {
		s.r.agg.bump(ws)
	}
	return nil
}

func alreadyLinked(relations, inserted []lead.Relation, lines map[string]int) []lead.ImportIssue {
	written := make(map[string]bool, len(inserted))
	for _, r := range inserted {
		written[r.ID] = true
	}
	var issues []lead.ImportIssue
	for _, r := range relations {
		if !written[r.ID] {
			issues = append(issues, lead.ImportIssue{Line: lines[r.ID], Reason: lead.ReasonRelationExists, Field: lead.ImportFieldRelative})
		}
	}
	return issues
}

type linkCandidate struct {
	line     int
	relation lead.Relation
}

func splitLinks(links []leadimport.LinkWrite) ([]lead.ImportIssue, []linkCandidate, pq.StringArray) {
	var failed []lead.ImportIssue
	var candidates []linkCandidate
	done := make(pq.StringArray, 0, len(links))
	seen := map[string]bool{}
	for _, l := range links {
		done = append(done, l.Link.ID)
		switch {
		case l.Issue != nil:
			failed = append(failed, *l.Issue)
		case l.Relation != nil:
			key := pairKey(*l.Relation)
			if seen[key] {
				failed = append(failed, lead.ImportIssue{Line: l.Link.Line, Reason: lead.ReasonRelationExists, Field: lead.ImportFieldRelative})
				continue
			}
			seen[key] = true
			candidates = append(candidates, linkCandidate{line: l.Link.Line, relation: *l.Relation})
		}
	}
	return failed, candidates, done
}

func pairKey(r lead.Relation) string {
	a, b := r.LeadID, r.OtherLeadID
	if b < a {
		a, b = b, a
	}
	return a + "|" + b + "|" + string(r.Dimension())
}

func lockedSides(tx *gorm.DB, ws string, candidates []linkCandidate) (map[string]bool, error) {
	present := map[string]bool{}
	if len(candidates) == 0 {
		return present, nil
	}
	var sides []string
	for _, c := range candidates {
		sides = append(sides, c.relation.LeadID, c.relation.OtherLeadID)
	}
	var locked []string
	if err := tx.Raw(lockLeadsSQL, ws, pq.StringArray(uniqueSorted(sides))).Scan(&locked).Error; err != nil {
		return nil, err
	}
	for _, id := range locked {
		present[id] = true
	}
	return present, nil
}

func insertImportRelations(tx *gorm.DB, ws string, relations []lead.Relation, now time.Time) ([]lead.Relation, error) {
	if len(relations) == 0 {
		return nil, nil
	}
	n := len(relations)
	ids, leads, others := make(pq.StringArray, n), make(pq.StringArray, n), make(pq.StringArray, n)
	dimensions, kinds, creators := make(pq.StringArray, n), make(pq.StringArray, n), make(pq.StringArray, n)
	byID := make(map[string]lead.Relation, n)
	for i, r := range relations {
		ids[i], leads[i], others[i] = r.ID, r.LeadID, r.OtherLeadID
		dimensions[i], kinds[i], creators[i] = string(r.Dimension()), string(r.Kind), humanActor(r.CreatedBy)
		byID[r.ID] = r
	}
	var inserted []string
	if err := tx.Raw(importInsertRelationsSQL, ws, now, ids, leads, others, dimensions, kinds, creators).Scan(&inserted).Error; err != nil {
		return nil, err
	}
	out := make([]lead.Relation, 0, len(inserted))
	for _, id := range inserted {
		out = append(out, byID[id])
	}
	return out, nil
}
