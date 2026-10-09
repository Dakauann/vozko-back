package lead

import (
	"context"
	"strings"
	"time"

	"github.com/lib/pq"
	"gorm.io/gorm"

	"vozko/domain/lead"
	"vozko/domain/recordevent"
	"vozko/domain/shared"
	"vozko/infra/database/schema"
)

const (
	relationCounterpartsSQL = "SELECT CASE WHEN lead_id = ? THEN other_lead_id ELSE lead_id END::text FROM lead_relations" +
		" WHERE workspace_id = ? AND (lead_id = ? OR other_lead_id = ?)"

	anonymizedLeadSQL = "SELECT * FROM leads WHERE id = ? AND workspace_id = ? AND deleted_at IS NULL"

	contactNumbersSQL = "SELECT number FROM lead_phones WHERE workspace_id = ? AND lead_id = ?"

	unofficialEntryNumbersSQL = "SELECT DISTINCT number FROM unofficial_whatsapp_campaign_entries WHERE workspace_id = ? AND lead_id = ?"

	unrelateAllSQL = "DELETE FROM lead_relations WHERE workspace_id = ? AND (lead_id = ? OR other_lead_id = ?) RETURNING *"

	heldByOthersSQL = "SELECT f.form FROM unnest(?::text[]) AS f(form)" +
		" WHERE EXISTS (SELECT 1 FROM leads WHERE leads.workspace_id = ? AND leads.number = f.form AND leads.id <> ? AND leads.deleted_at IS NULL)" +
		" OR EXISTS (SELECT 1 FROM lead_phones p JOIN leads holder ON holder.id = p.lead_id AND holder.deleted_at IS NULL" +
		" WHERE p.workspace_id = ? AND p.number = f.form AND p.lead_id <> ?)"

	erasePhonesSQL = "DELETE FROM lead_phones WHERE workspace_id = ? AND lead_id = ?"

	eraseAddressesSQL = "DELETE FROM lead_addresses WHERE workspace_id = ? AND lead_id = ?"

	eraseMemoriesSQL = "DELETE FROM lead_memories WHERE workspace_id = ? AND lead_id = ?"

	redactLeadEventsSQL = "UPDATE lead_events SET changes = CASE WHEN jsonb_typeof(changes) = 'array' THEN" +
		" COALESCE((SELECT jsonb_agg(jsonb_build_object('field', item.change -> 'field', 'before', NULL, 'after', NULL, 'redacted', true) ORDER BY item.position)" +
		" FROM jsonb_array_elements(changes) WITH ORDINALITY AS item(change, position)), '[]'::jsonb) ELSE '[]'::jsonb END" +
		" WHERE workspace_id = ? AND lead_id = ?"

	eraseCallListItemsSQL = "UPDATE call_list_items SET phone = '', note = NULL WHERE workspace_id = ? AND lead_id = ?"

	eraseCampaignEntriesSQL = "UPDATE whatsapp_campaign_entries SET variables = NULL, metadata = '{}'::jsonb WHERE lead_id = ?"

	anonymizeLeadSQL = "UPDATE leads SET " + recordColumnsSQL + ", relatives_count = 0, referred_count = 0, updated_at = ?, deleted_at = ?, version = version + 1" +
		" WHERE id = ? AND workspace_id = ? AND deleted_at IS NULL RETURNING version"
)

var (
	eraseUnofficialEntriesSQL = "UPDATE unofficial_whatsapp_campaign_entries SET variables = NULL, metadata = '{}'::jsonb, name = ''," +
		" number = " + maskedColumn("unofficial_whatsapp_campaign_entries.number") + " WHERE workspace_id = ? AND lead_id = ?"

	maskCallsSQL = "UPDATE calls SET phone_from = " + maskedColumn("calls.phone_from") + ", phone_to = " + maskedColumn("calls.phone_to") +
		" WHERE calls.workspace_id = ? AND (calls.phone_from = ANY(?) OR calls.phone_to = ANY(?))"

	maskOwnCallsSQL = maskCallsSQL + " AND calls.lead_id = ?"

	maskUnownedCallsSQL = maskCallsSQL + " AND calls.lead_id IS NULL"

	maskSendsSQL = "UPDATE whatsapp_template_sends SET to_number = " + maskedColumn("whatsapp_template_sends.to_number") +
		" WHERE whatsapp_template_sends.workspace_id = ? AND whatsapp_template_sends.to_number = ANY(?)"

	maskOwnSendsSQL = maskSendsSQL + " AND whatsapp_template_sends.entry_id IN (SELECT id FROM whatsapp_campaign_entries WHERE lead_id = ?)"

	maskUnownedSendsSQL = maskSendsSQL + " AND whatsapp_template_sends.entry_id IS NULL"
)

func maskedColumn(column string) string {
	return "COALESCE((SELECT m.masked FROM unnest(?::text[], ?::text[]) AS m(form, masked) WHERE m.form = " + column + " LIMIT 1), " + column + ")"
}

type erasureStep struct {
	target lead.ErasureTarget
	sql    string
}

var leadScopedErasures = []erasureStep{
	{lead.ErasurePhones, erasePhonesSQL},
	{lead.ErasureAddresses, eraseAddressesSQL},
	{lead.ErasureMemories, eraseMemoriesSQL},
	{lead.ErasureEvents, redactLeadEventsSQL},
	{lead.ErasureCallListItems, eraseCallListItemsSQL},
}

type maskMapping struct {
	forms, masked pq.StringArray
}

func mappingOf(masks []lead.NumberMask) maskMapping {
	m := maskMapping{forms: pq.StringArray{}, masked: pq.StringArray{}}
	for _, mask := range masks {
		for _, form := range mask.Forms {
			m.forms = append(m.forms, form)
			m.masked = append(m.masked, mask.Masked)
		}
	}
	return m
}

type anonymization struct {
	tx      *gorm.DB
	subject *lead.Lead
	actorID string
	at      time.Time
	erasure lead.Erasure
}

func (r *repository) Anonymize(ctx context.Context, workspaceID, leadID, actorID string, at time.Time) (lead.Erasure, error) {
	workspaceID, leadID = strings.TrimSpace(workspaceID), strings.TrimSpace(leadID)
	if workspaceID == "" {
		return lead.Erasure{}, lead.ErrLeadWorkspaceRequired
	}
	if leadID == "" {
		return lead.Erasure{}, lead.ErrLeadRequired
	}
	var erasure lead.Erasure
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		run := &anonymization{
			tx:      tx,
			actorID: actorID,
			at:      at,
			erasure: lead.Erasure{LeadID: leadID, At: at, Rows: map[lead.ErasureTarget]int64{}},
		}
		if err := run.erase(workspaceID, leadID); err != nil {
			return err
		}
		erasure = run.erasure
		return nil
	})
	if err != nil {
		return lead.Erasure{}, err
	}
	r.agg.bump(workspaceID)
	return erasure, nil
}

func (a *anonymization) erase(workspaceID, leadID string) error {
	counterparts, err := a.counterparts(workspaceID, leadID)
	if err != nil {
		return err
	}
	if err := a.lock(workspaceID, leadID, counterparts); err != nil {
		return err
	}
	entryNumbers, err := a.numbers(unofficialEntryNumbersSQL)
	if err != nil {
		return err
	}
	if err := a.unrelate(); err != nil {
		return err
	}
	masks := a.subject.NumberMasks()
	erasable, err := a.erasable(masks)
	if err != nil {
		return err
	}
	ws, id := a.subject.WorkspaceID, a.subject.ID
	addressTexts, err := a.numbers(heldFingerprintsSQL)
	if err != nil {
		return err
	}
	for _, step := range leadScopedErasures {
		if err := a.exec(step.target, step.sql, ws, id); err != nil {
			return err
		}
	}
	released, err := releaseStoredAnswers(a.tx, ws, addressTexts)
	if err != nil {
		return err
	}
	a.erasure.Rows[lead.ErasureGeocodeCache] = released
	if err := a.exec(lead.ErasureCampaignEntries, eraseCampaignEntriesSQL, a.subject.ID); err != nil {
		return err
	}
	if err := a.eraseUnofficialEntries(entryNumbers); err != nil {
		return err
	}
	if err := a.maskNumbers(masks, erasable); err != nil {
		return err
	}
	if err := a.eraseRecord(); err != nil {
		return err
	}
	return insertEvents(a.tx, a.subject.WorkspaceID, a.subject.ID, []recordevent.Event{lead.AnonymizationEvent(a.actorID)}, a.at)
}

func (a *anonymization) counterparts(workspaceID, leadID string) ([]string, error) {
	var counterparts []string
	if err := a.tx.Raw(relationCounterpartsSQL, leadID, workspaceID, leadID, leadID).Scan(&counterparts).Error; err != nil {
		return nil, err
	}
	return counterparts, nil
}

func (a *anonymization) lock(workspaceID, leadID string, counterparts []string) error {
	if err := lockLeads(a.tx, workspaceID, append(counterparts, leadID), nil); err != nil {
		return err
	}
	var rows []schema.Lead
	if err := a.tx.Raw(anonymizedLeadSQL, leadID, workspaceID).Scan(&rows).Error; err != nil {
		return err
	}
	if len(rows) == 0 {
		return lead.ErrLeadNotFound
	}
	subject, err := toDomain(&rows[0])
	if err != nil {
		return err
	}
	a.subject = subject
	contacts, err := a.numbers(contactNumbersSQL)
	if err != nil {
		return err
	}
	subject.Phones = make([]lead.ContactPhone, 0, len(contacts))
	for _, number := range contacts {
		subject.Phones = append(subject.Phones, lead.ContactPhone{Number: number})
	}
	return nil
}

func (a *anonymization) numbers(query string) ([]string, error) {
	var numbers []string
	if err := a.tx.Raw(query, a.subject.WorkspaceID, a.subject.ID).Scan(&numbers).Error; err != nil {
		return nil, err
	}
	return numbers, nil
}

func (a *anonymization) unrelate() error {
	var rows []schema.LeadRelation
	if err := a.tx.Raw(unrelateAllSQL, a.subject.WorkspaceID, a.subject.ID, a.subject.ID).Scan(&rows).Error; err != nil {
		return err
	}
	a.erasure.Rows[lead.ErasureRelations] = int64(len(rows))
	if len(rows) == 0 {
		return nil
	}
	change := relationChange{removedBy: a.actorID}
	for _, row := range rows {
		change.removed = append(change.removed, relationOf(row))
	}
	tallies, err := applyRelationChange(a.tx, a.subject.WorkspaceID, change, a.subject.ID, a.at)
	a.erasure.Counterparts = tallies
	return err
}

func (a *anonymization) erasable(masks []lead.NumberMask) ([]lead.NumberMask, error) {
	contactForms := lead.ContactForms(masks)
	if len(contactForms) == 0 {
		return masks, nil
	}
	var held []string
	if err := a.tx.Raw(heldByOthersSQL, pq.StringArray(contactForms), a.subject.WorkspaceID, a.subject.ID, a.subject.WorkspaceID, a.subject.ID).
		Scan(&held).Error; err != nil {
		return nil, err
	}
	return lead.ErasableMasks(masks, held), nil
}

func (a *anonymization) eraseUnofficialEntries(numbers []string) error {
	masks := make([]lead.NumberMask, 0, len(numbers))
	for _, number := range numbers {
		masks = append(masks, lead.NumberMask{Masked: shared.MaskContact(number), Forms: []string{number}})
	}
	m := mappingOf(masks)
	return a.exec(lead.ErasureUnofficialCampaignEntries, eraseUnofficialEntriesSQL, m.forms, m.masked, a.subject.WorkspaceID, a.subject.ID)
}

func (a *anonymization) maskNumbers(own, unowned []lead.NumberMask) error {
	a.erasure.Rows[lead.ErasureCallNumbers] = 0
	a.erasure.Rows[lead.ErasureTemplateSendNumbers] = 0
	ws, id := a.subject.WorkspaceID, a.subject.ID
	mine, theirs := mappingOf(own), mappingOf(unowned)
	if len(mine.forms) == 0 {
		return nil
	}
	steps := []struct {
		target lead.ErasureTarget
		sql    string
		args   []interface{}
	}{
		{lead.ErasureCallNumbers, maskOwnCallsSQL, []interface{}{mine.forms, mine.masked, mine.forms, mine.masked, ws, mine.forms, mine.forms, id}},
		{lead.ErasureCallNumbers, maskUnownedCallsSQL, []interface{}{theirs.forms, theirs.masked, theirs.forms, theirs.masked, ws, theirs.forms, theirs.forms}},
		{lead.ErasureTemplateSendNumbers, maskOwnSendsSQL, []interface{}{mine.forms, mine.masked, ws, mine.forms, id}},
		{lead.ErasureTemplateSendNumbers, maskUnownedSendsSQL, []interface{}{theirs.forms, theirs.masked, ws, theirs.forms}},
	}
	for _, step := range steps {
		if err := a.exec(step.target, step.sql, step.args...); err != nil {
			return err
		}
	}
	return nil
}

func (a *anonymization) exec(target lead.ErasureTarget, query string, args ...interface{}) error {
	result := a.tx.Exec(query, args...)
	if result.Error != nil {
		return result.Error
	}
	a.erasure.Rows[target] += result.RowsAffected
	return nil
}

func (a *anonymization) eraseRecord() error {
	columns, err := recordColumns(a.subject.Anonymized())
	if err != nil {
		return err
	}
	var versions []int64
	args := append(columns, a.at, a.at, a.subject.ID, a.subject.WorkspaceID)
	if err := a.tx.Raw(anonymizeLeadSQL, args...).Scan(&versions).Error; err != nil {
		return err
	}
	if len(versions) == 0 {
		return lead.ErrLeadNotFound
	}
	a.erasure.Version = versions[0]
	a.erasure.Rows[lead.ErasureRecord] = 1
	return nil
}
