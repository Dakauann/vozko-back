package lead

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/lib/pq"
	"gorm.io/gorm"

	"vozko/domain/actor"
	"vozko/domain/lead"
	"vozko/domain/recordevent"
	"vozko/domain/shared"
	"vozko/infra/database"
	"vozko/infra/database/schema"
	infracrmfilter "vozko/infra/repositories/crmfilter"
)

const (
	duplicateLookupLimit = 200
	directoryLookupLimit = 500

	lockLeadsFromSQL = " FROM leads WHERE workspace_id = ? AND id = ANY(?::uuid[]) AND deleted_at IS NULL ORDER BY id FOR UPDATE"

	lockLeadsSQL = "SELECT id::text" + lockLeadsFromSQL

	shiftRelationCountsSQL = "UPDATE leads SET relatives_count = leads.relatives_count + d.relatives, referred_count = leads.referred_count + d.referred," +
		" version = leads.version + 1, updated_at = ?" +
		" FROM unnest(?::uuid[], ?::int[], ?::int[]) AS d(id, relatives, referred)" +
		" WHERE leads.id = d.id AND leads.workspace_id = ? AND leads.deleted_at IS NULL" +
		" RETURNING leads.id::text AS id, leads.version, leads.relatives_count, leads.referred_count"

	addressColumnsSQL = "label = ?, is_primary = ?, position = ?, zip_code = ?, street = ?, number = ?, complement = ?, district = ?, district_key = ?," +
		" city = ?, city_key = ?, city_code = ?, state = ?, latitude = ?, longitude = ?, geo_precision = ?, geo_source = ?, geo_provider = ?," +
		" geo_status = ?, geocoded_at = ?, fingerprint = ?, updated_at = ?"

	updateAddressSQL = "UPDATE lead_addresses SET " + addressColumnsSQL +
		" WHERE id = ? AND lead_id = ? AND workspace_id = ?"

	requeueAddressSQL = "UPDATE lead_addresses SET " + addressColumnsSQL + ", geo_next_at = ?, geo_attempts = 0, geo_claim = NULL" +
		" WHERE id = ? AND lead_id = ? AND workspace_id = ?"

	repositionAddressSQL = "UPDATE lead_addresses SET " + addressColumnsSQL + ", geo_claim = NULL" +
		" WHERE id = ? AND lead_id = ? AND workspace_id = ?"

	promoteIdentitySQL = "UPDATE leads SET number = ?, updated_at = ?, version = version + 1" +
		" WHERE id = ? AND workspace_id = ? AND number IS NULL AND version = ? AND deleted_at IS NULL RETURNING version"

	numberHoldersSQL = "SELECT id FROM leads WHERE workspace_id = ? AND number = ANY(?) AND deleted_at IS NULL" +
		" UNION ALL SELECT lead_id FROM lead_phones WHERE workspace_id = ? AND number = ANY(?)"

	holdingNumbersSQL = "SELECT * FROM leads WHERE leads.workspace_id = ? AND leads.deleted_at IS NULL AND leads.id IN (" + numberHoldersSQL + ")" +
		" ORDER BY (leads.number = ANY(?)) IS TRUE DESC, leads.id LIMIT ?"

	livingAtSQL = "SELECT * FROM leads WHERE leads.workspace_id = ? AND leads.deleted_at IS NULL" +
		" AND leads.id IN (SELECT lead_id FROM lead_addresses WHERE workspace_id = ? AND fingerprint = ANY(?))" +
		" ORDER BY leads.id LIMIT ?"
)

var errPromotionLost = errors.New("lead: the contact phone was promoted or taken by someone else first")

type collections struct {
	phones, addresses bool
}

var (
	recordAggregate = collections{phones: true, addresses: true}
	phonesOnly      = collections{phones: true}
)

func (r *repository) Load(ctx context.Context, workspaceID, id string) (*lead.Lead, error) {
	return r.loadWith(ctx, workspaceID, id, recordAggregate)
}

func (r *repository) loadWith(ctx context.Context, workspaceID, id string, which collections) (*lead.Lead, error) {
	l, err := r.FindByID(workspaceID, id)
	if err != nil {
		return nil, err
	}
	if err := attachCollections(r.db.WithContext(ctx), workspaceID, []*lead.Lead{l}, which); err != nil {
		return nil, err
	}
	return l, nil
}

func attachCollections(db *gorm.DB, workspaceID string, leads []*lead.Lead, which collections) error {
	if len(leads) == 0 {
		return nil
	}
	byID := make(map[string]*lead.Lead, len(leads))
	ids := make(pq.StringArray, 0, len(leads))
	for _, l := range leads {
		byID[l.ID] = l
		ids = append(ids, l.ID)
		if which.phones {
			l.Phones = []lead.ContactPhone{}
		}
		if which.addresses {
			l.Addresses = []lead.Address{}
		}
	}
	if which.phones {
		var rows []schema.LeadPhone
		if err := db.Where("workspace_id = ? AND lead_id = ANY(?::uuid[])", workspaceID, ids).Order("lead_id, position").Find(&rows).Error; err != nil {
			return fmt.Errorf("lead phones: %w", err)
		}
		for _, row := range rows {
			byID[row.LeadID].Phones = append(byID[row.LeadID].Phones, phoneOf(row))
		}
	}
	if which.addresses {
		var rows []schema.LeadAddress
		if err := db.Where("workspace_id = ? AND lead_id = ANY(?::uuid[])", workspaceID, ids).Order("lead_id, position").Find(&rows).Error; err != nil {
			return fmt.Errorf("lead addresses: %w", err)
		}
		for _, row := range rows {
			byID[row.LeadID].Addresses = append(byID[row.LeadID].Addresses, addressOf(row))
		}
	}
	return nil
}

type relationChange struct {
	added     []lead.Relation
	removed   []lead.Relation
	removedBy string
}

func sidesOf(r lead.Relation, settled string) []string {
	sides := make([]string, 0, 2)
	for _, side := range []string{r.LeadID, r.OtherLeadID} {
		if side != settled {
			sides = append(sides, side)
		}
	}
	return sides
}

func (c relationChange) deltas(settled string) map[string]lead.RelationCounts {
	deltas := map[string]lead.RelationCounts{}
	for _, r := range c.added {
		for _, side := range sidesOf(r, settled) {
			deltas[side] = deltas[side].Plus(r.CountsFor(side))
		}
	}
	for _, r := range c.removed {
		for _, side := range sidesOf(r, settled) {
			deltas[side] = deltas[side].Minus(r.CountsFor(side))
		}
	}
	return deltas
}

func (c relationChange) sidesOtherThan(settled string) []string {
	var sides []string
	for _, r := range c.added {
		sides = append(sides, sidesOf(r, settled)...)
	}
	for _, r := range c.removed {
		sides = append(sides, sidesOf(r, settled)...)
	}
	return uniqueSorted(sides)
}

func (c relationChange) eventRows(workspaceID, settled string, at time.Time) ([]schema.LeadEvent, error) {
	var rows []schema.LeadEvent
	add := func(kind recordevent.Kind, by string, r lead.Relation) error {
		for _, side := range sidesOf(r, settled) {
			sideRows, err := eventRows(workspaceID, side, []recordevent.Event{lead.RelationEvent(kind, by, side, r)}, at)
			if err != nil {
				return err
			}
			rows = append(rows, sideRows...)
		}
		return nil
	}
	for _, r := range c.added {
		if err := add(lead.EventRelationAdded, r.CreatedBy, r); err != nil {
			return nil, err
		}
	}
	for _, r := range c.removed {
		if err := add(lead.EventRelationRemoved, c.removedBy, r); err != nil {
			return nil, err
		}
	}
	return rows, nil
}

func (r *repository) newRelations(l *lead.Lead, now time.Time) ([]lead.Relation, error) {
	added := make([]lead.Relation, 0, len(l.Relations))
	for i := range l.Relations {
		rel := &l.Relations[i]
		if !rel.Involves(l.ID) {
			return nil, fmt.Errorf("lead %s carries relation %s of other leads", l.ID, rel.ID)
		}
		if rel.ID != "" {
			return nil, fmt.Errorf("the new lead %s cannot carry the stored relation %s", l.ID, rel.ID)
		}
		rel.ID, rel.CreatedAt = r.leadID(), now
		added = append(added, *rel)
	}
	return added, nil
}

func insertRelationRows(tx *gorm.DB, workspaceID string, relations []lead.Relation) error {
	if len(relations) == 0 {
		return nil
	}
	rows := make([]schema.LeadRelation, 0, len(relations))
	for _, rel := range relations {
		rows = append(rows, relationRow(workspaceID, rel))
	}
	if err := tx.Create(&rows).Error; err != nil {
		if database.IsUniqueViolation(err) {
			return lead.ErrRelationExists
		}
		return err
	}
	return nil
}

func applyRelationChange(tx *gorm.DB, workspaceID string, change relationChange, settled string, now time.Time) (map[string]lead.RelationTally, error) {
	tallies, err := shiftRelationCounts(tx, workspaceID, change.deltas(settled), now)
	if err != nil {
		return nil, err
	}
	rows, err := change.eventRows(workspaceID, settled, now)
	if err != nil {
		return nil, err
	}
	if err := insertEventRows(tx, rows); err != nil {
		return nil, err
	}
	return tallies, nil
}

func lockLeads(tx *gorm.DB, workspaceID string, ids []string, required []string) error {
	var locked []string
	if err := tx.Raw(lockLeadsSQL, workspaceID, pq.StringArray(uniqueSorted(ids))).Scan(&locked).Error; err != nil {
		return err
	}
	present := make(map[string]bool, len(locked))
	for _, id := range locked {
		present[id] = true
	}
	for _, id := range required {
		if !present[id] {
			return lead.ErrRelativeNotFound
		}
	}
	return nil
}

func uniqueSorted(ids []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id != "" && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

type tallyRow struct {
	ID             string
	Version        int64
	RelativesCount int
	ReferredCount  int
}

func shiftRelationCounts(tx *gorm.DB, workspaceID string, deltas map[string]lead.RelationCounts, now time.Time) (map[string]lead.RelationTally, error) {
	tallies := map[string]lead.RelationTally{}
	if len(deltas) == 0 {
		return tallies, nil
	}
	ids := make([]string, 0, len(deltas))
	for id := range deltas {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	relatives := make(pq.Int64Array, 0, len(ids))
	referred := make(pq.Int64Array, 0, len(ids))
	for _, id := range ids {
		relatives = append(relatives, int64(deltas[id].Relatives))
		referred = append(referred, int64(deltas[id].Referred))
	}
	var rows []tallyRow
	if err := tx.Raw(shiftRelationCountsSQL, now, pq.StringArray(ids), relatives, referred, workspaceID).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		tallies[row.ID] = lead.RelationTally{Version: row.Version, Counts: lead.RelationCounts{Relatives: row.RelativesCount, Referred: row.ReferredCount}}
	}
	return tallies, nil
}

func writePhones(tx *gorm.DB, l *lead.Lead, now time.Time, replacing bool, newID func() string) error {
	for i := range l.Phones {
		if l.Phones[i].ID == "" {
			l.Phones[i].ID = newID()
		}
	}
	next := phoneRows(l, now)
	if replacing {
		var stored []schema.LeadPhone
		if err := tx.Where("workspace_id = ? AND lead_id = ?", l.WorkspaceID, l.ID).Order("position").Find(&stored).Error; err != nil {
			return fmt.Errorf("stored phones of lead %s: %w", l.ID, err)
		}
		if samePhones(stored, next) {
			return nil
		}
		if err := tx.Exec("DELETE FROM lead_phones WHERE workspace_id = ? AND lead_id = ?", l.WorkspaceID, l.ID).Error; err != nil {
			return err
		}
	}
	if len(next) == 0 {
		return nil
	}
	if err := tx.Create(&next).Error; err != nil {
		return err
	}
	for i := range l.Phones {
		l.Phones[i].CreatedAt = next[i].CreatedAt
	}
	return nil
}

func writeAddresses(tx *gorm.DB, l *lead.Lead, now time.Time, replacing bool, newID func() string) error {
	stored := map[string]schema.LeadAddress{}
	if replacing {
		var rows []schema.LeadAddress
		if err := tx.Where("workspace_id = ? AND lead_id = ?", l.WorkspaceID, l.ID).Find(&rows).Error; err != nil {
			return fmt.Errorf("stored addresses of lead %s: %w", l.ID, err)
		}
		for _, row := range rows {
			stored[row.ID] = row
		}
	}
	var inserts, updates []schema.LeadAddress
	kept, holds := map[string]bool{}, map[string]bool{}
	for i := range l.Addresses {
		a := &l.Addresses[i]
		if a.ID == "" {
			a.ID = newID()
			row := addressRow(l, i, *a, now)
			a.CreatedAt = row.CreatedAt
			holds[row.Fingerprint] = true
			inserts = append(inserts, row)
			continue
		}
		previous, ok := stored[a.ID]
		if !ok {
			return shared.ErrVersionConflict
		}
		kept[a.ID] = true
		row := addressRow(l, i, *a, now)
		holds[row.Fingerprint] = true
		if !sameAddress(previous, row) {
			updates = append(updates, row)
		}
	}
	var removed pq.StringArray
	var left []string
	for id, row := range stored {
		if !kept[id] {
			removed = append(removed, id)
		}
		if !holds[row.Fingerprint] {
			left = append(left, row.Fingerprint)
		}
	}
	if len(removed) > 0 {
		if err := tx.Exec("DELETE FROM lead_addresses WHERE workspace_id = ? AND lead_id = ? AND id = ANY(?::uuid[])", l.WorkspaceID, l.ID, removed).Error; err != nil {
			return err
		}
	}
	for _, primary := range []bool{false, true} {
		for _, row := range updates {
			if row.IsPrimary != primary {
				continue
			}
			if err := updateAddress(tx, row, stored[row.ID]); err != nil {
				return err
			}
		}
		for i := range inserts {
			if inserts[i].IsPrimary != primary {
				continue
			}
			if err := tx.Create(&inserts[i]).Error; err != nil {
				return err
			}
		}
	}
	_, err := releaseStoredAnswers(tx, l.WorkspaceID, left)
	return err
}

func updateAddress(tx *gorm.DB, row, previous schema.LeadAddress) error {
	args := []interface{}{
		row.Label, row.IsPrimary, row.Position, row.ZipCode, row.Street, row.Number, row.Complement, row.District, row.DistrictKey,
		row.City, row.CityKey, row.CityCode, row.State, row.Latitude, row.Longitude, row.GeoPrecision, row.GeoSource, row.GeoProvider,
		row.GeoStatus, row.GeocodedAt, row.Fingerprint, row.UpdatedAt,
	}
	sql := updateAddressSQL
	switch {
	case row.Fingerprint != previous.Fingerprint && row.GeoStatus == string(lead.GeoPending):
		sql = requeueAddressSQL
		args = append(args, row.GeoNextAt)
	case positionChanged(row, previous):
		sql = repositionAddressSQL
	}
	args = append(args, row.ID, row.LeadID, row.WorkspaceID)
	return tx.Exec(sql, args...).Error
}

func positionChanged(row, previous schema.LeadAddress) bool {
	return row.GeoSource != previous.GeoSource || row.GeoPrecision != previous.GeoPrecision ||
		!sameFloat(row.Latitude, previous.Latitude) || !sameFloat(row.Longitude, previous.Longitude)
}

func (r *repository) HasEntries(ctx context.Context, workspaceID, leadID string) (bool, error) {
	workspaceID, leadID = strings.TrimSpace(workspaceID), strings.TrimSpace(leadID)
	if workspaceID == "" {
		return false, lead.ErrLeadWorkspaceRequired
	}
	if leadID == "" {
		return false, lead.ErrLeadRequired
	}
	sql := "SELECT EXISTS (SELECT 1 FROM " + infracrmfilter.LeadEntriesSource() +
		" WHERE lead_entries.lead_id = ? AND EXISTS (SELECT 1 FROM leads WHERE leads.id = ? AND leads.workspace_id = ?))"
	var used bool
	if err := r.db.WithContext(ctx).Raw(sql, leadID, leadID, workspaceID).Scan(&used).Error; err != nil {
		return false, err
	}
	return used, nil
}

func numberFormats(numbers []string) pq.StringArray {
	seen := map[string]bool{}
	formats := pq.StringArray{}
	for _, number := range numbers {
		for _, format := range lead.NumberFormats(number) {
			if !seen[format] {
				seen[format] = true
				formats = append(formats, format)
			}
		}
	}
	return formats
}

func (r *repository) numberHolders(db *gorm.DB, workspaceID string, formats pq.StringArray, limit int) ([]schema.Lead, error) {
	var rows []schema.Lead
	if err := db.Raw(holdingNumbersSQL, workspaceID, workspaceID, formats, workspaceID, formats, formats, limit).Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *repository) FindByNumbersOrAddresses(ctx context.Context, workspaceID string, numbers, fingerprints []string) ([]*lead.Lead, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, lead.ErrLeadWorkspaceRequired
	}
	db := r.db.WithContext(ctx)
	byID := map[string]schema.Lead{}
	var order []string
	collect := func(rows []schema.Lead) {
		for _, row := range rows {
			if _, seen := byID[row.ID]; !seen {
				byID[row.ID] = row
				order = append(order, row.ID)
			}
		}
	}
	if formats := numberFormats(numbers); len(formats) > 0 {
		rows, err := r.numberHolders(db, workspaceID, formats, duplicateLookupLimit)
		if err != nil {
			return nil, err
		}
		collect(rows)
	}
	if prints := uniqueSorted(fingerprints); len(prints) > 0 {
		var rows []schema.Lead
		if err := db.Raw(livingAtSQL, workspaceID, workspaceID, pq.StringArray(prints), duplicateLookupLimit).Scan(&rows).Error; err != nil {
			return nil, err
		}
		collect(rows)
	}
	rows := make([]schema.Lead, 0, len(order))
	for _, id := range order {
		rows = append(rows, byID[id])
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

func (r *repository) promotionCandidate(workspaceID, number string, formats pq.StringArray) (*lead.Lead, error) {
	var rows []schema.Lead
	err := r.scope(workspaceID).Where("number IS NULL").
		Where("id IN (SELECT lead_id FROM lead_phones WHERE workspace_id = ? AND number = ANY(?))", workspaceID, formats).
		Limit(2).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	holders, err := toDomainAll(rows)
	if err != nil {
		return nil, err
	}
	if err := attachCollections(r.db, workspaceID, holders, phonesOnly); err != nil {
		return nil, err
	}
	return lead.PromotionCandidate(number, holders), nil
}

func (r *repository) promoteIdentity(workspaceID, number string, formats pq.StringArray) (*lead.Lead, error) {
	candidate, err := r.promotionCandidate(workspaceID, number, formats)
	if err != nil || candidate == nil {
		return nil, err
	}
	before := *candidate
	if err := candidate.PromoteContactPhone(number); err != nil {
		return nil, err
	}
	event := lead.Changes(lead.EventIdentityPromoted, actor.SystemID, &before, candidate, nil)
	now := time.Now().UTC()
	err = r.db.Transaction(func(tx *gorm.DB) error {
		var versions []int64
		if err := tx.Raw(promoteIdentitySQL, candidate.Number, now, candidate.ID, workspaceID, before.Version).Scan(&versions).Error; err != nil {
			if database.IsUniqueViolation(err) {
				return errPromotionLost
			}
			return err
		}
		if len(versions) == 0 {
			return errPromotionLost
		}
		candidate.Version, candidate.UpdatedAt = versions[0], now
		if err := tx.Exec("DELETE FROM lead_phones WHERE workspace_id = ? AND lead_id = ? AND number = ANY(?)", workspaceID, candidate.ID, formats).Error; err != nil {
			return err
		}
		return insertEvents(tx, workspaceID, candidate.ID, []recordevent.Event{event}, now)
	})
	if errors.Is(err, errPromotionLost) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r.agg.bump(workspaceID)
	return candidate, nil
}
