package database

import (
	"fmt"

	"gorm.io/gorm"
)

// Data repairs: one-off corrections to rows a shipped bug produced.
//
// They live beside the schema rather than in a script because the constraint
// that prevents a bug from recurring cannot be created while the broken rows are
// still there, and a constraint that fails to build aborts the boot. Repair then
// constrain, in one transaction, is the only ordering that leaves the database
// in a state the code can rely on.
//
// Every repair below MUST be idempotent and MUST be a no-op on a database that
// never had the defect: this runs on every boot, forever.

func runDataRepairs(tx *gorm.DB) error {
	repairs := []struct {
		name string
		run  func(*gorm.DB) error
	}{
		{"uw_clear_group_contact_phone_numbers", clearGroupContactPhoneNumbers},
		{"uw_merge_split_group_conversations", mergeSplitGroupConversations},
		{"uw_retire_unattributable_conversations", retireUnattributableConversations},
		{"uw_reset_never_read_profile_clocks", resetNeverReadProfileClocks},
	}

	for _, r := range repairs {
		if err := r.run(tx); err != nil {
			return fmt.Errorf("data repair %s: %w", r.name, err)
		}
	}
	return nil
}

// clearGroupContactPhoneNumbers erases the fake numbers a group id produced.
//
// PhoneFromJID used to strip the domain off any JID, so "120363…@g.us" yielded
// "120363…" and was stored as a phone number. Those rows render as "+120363…"
// in the CRM's number column, and are addressable by the dialer and by the lead
// bridge — neither of which can reach a group.
//
// The type flag is set in the same pass, so a group subject that predates the
// column is correctly typed rather than waiting for its next inbound message.
// Runs BEFORE the conversation merge, which relies on group subjects being
// identifiable.
func clearGroupContactPhoneNumbers(tx *gorm.DB) error {
	if !tx.Migrator().HasTable("unofficial_whatsapp_contacts") ||
		!tx.Migrator().HasColumn("unofficial_whatsapp_contacts", "is_group") {
		return nil
	}
	return tx.Exec(`
		UPDATE unofficial_whatsapp_contacts
		SET is_group = true, phone_number = '', lead_id = NULL
		WHERE jid LIKE '%@g.us'
		  AND (is_group = false OR phone_number <> '' OR lead_id IS NOT NULL)
	`).Error
}

// retireUnattributableConversations hides the catch-all row every instance grew.
//
// The defect: a webhook payload the decoder could not read yielded an event with
// no chat id and no sender, and the ingest path resolved it anyway — creating a
// contact with an EMPTY jid. Because contact identity is uniquely
// (instance, jid), every such event afterwards resolved to that same row, so one
// conversation per number accumulated every unattributable message. It rendered
// in the inbox titled with the raw entry type ("unofficial_whatsapp"), because a
// contact with no name and no handle falls through to that last-resort label,
// and its messages read "[mensagem sem conteúdo]" because they had no text
// either. The normalizer now refuses to attribute these at all.
//
// Soft-deleted, never dropped, and the MESSAGES are left alone. The conversation
// is unusable — its chat id is empty, so nothing can be sent to it — but the
// rows underneath are the only surviving record of payloads we failed to decode,
// and they are what a future investigation would need.
func retireUnattributableConversations(tx *gorm.DB) error {
	if !tx.Migrator().HasTable("unofficial_whatsapp_conversations") ||
		!tx.Migrator().HasTable("unofficial_whatsapp_contacts") {
		return nil
	}
	return tx.Exec(`
		UPDATE unofficial_whatsapp_conversations AS c
		SET deleted_at = NOW()
		FROM unofficial_whatsapp_contacts AS s
		WHERE s.id = c.contact_id
		  AND c.deleted_at IS NULL
		  AND COALESCE(c.chat_id, '') = ''
		  AND COALESCE(s.jid, '') = ''
	`).Error
}

// resetNeverReadProfileClocks un-stamps a staleness clock that was never earned.
//
// `profile_fetched_at` changed meaning. It used to be written by a NAME-only
// refresh — the push name that rode in on a message — even though no profile was
// ever read and no picture was ever stored. It now means "we asked the provider
// who this is", and the enrichment path is gated on it.
//
// Left alone, every contact created before this change claims a recent profile
// read, so the first real read is suppressed for a whole TTL: a customer with a
// perfectly good WhatsApp photo keeps rendering as initials for a week after the
// deploy, which is exactly the symptom this fixes.
//
// The predicate is the honest one — a subject with no stored picture was never
// profile-read, because nothing before this change could write that column.
//
// It re-runs on every boot for contacts whose read genuinely finds no picture
// (privacy settings hide it, or they simply have none), costing them one extra
// read per deploy. That is bounded and self-correcting, and much better than the
// alternative of never retrying a contact who later adds a photo.
func resetNeverReadProfileClocks(tx *gorm.DB) error {
	if !tx.Migrator().HasTable("unofficial_whatsapp_contacts") {
		return nil
	}
	return tx.Exec(`
		UPDATE unofficial_whatsapp_contacts
		SET profile_fetched_at = NULL
		WHERE profile_fetched_at IS NOT NULL
		  AND COALESCE(picture_url, '') = ''
		  AND deleted_at IS NULL
	`).Error
}

// entryTable is one table that keys rows by (entry_id, entry_type).
type entryTable struct {
	name string
	// idIsText marks the tables whose entry_id is text rather than uuid.
	// ai_attendance_sessions is one, because a voice session's entry can be a
	// SIP identifier. Casting the wrong way there fails the whole migration.
	idIsText bool
	// dedupeOn is what makes a row "already present" on the survivor, beyond the
	// entry itself. Empty means the entry alone is the key: at most one row.
	// Nil-and-absent (see appendOnly below) means duplicates are the normal
	// state and everything is repointed.
	dedupeOn []string
	// softDeleted marks tables carrying a deleted_at, so the "does the survivor
	// already have this" test does not count a row no read path can see.
	softDeleted bool
}

func (t entryTable) idExpr(column string) string {
	if t.idIsText {
		return column + "::text"
	}
	return column
}

// mergeSplitGroupConversations folds the duplicate conversations one WhatsApp
// group produced into a single entry.
//
// The defect: unofficial_whatsapp conversations were keyed by (instance,
// contact), and a group message's contact was resolved from its SENDER — a
// participant — rather than from the chat. So every member who spoke created
// another conversation for the same `chat_id`, each labelled with that member's
// name and number, and the lookup that resolves a chat for delivery receipts and
// calls picked one of them arbitrarily.
//
// The merge keeps the OLDEST row per (instance_id, chat_id): it holds the
// beginning of the thread, and any stage or label an operator attached is most
// likely on it. Everything entry-keyed is repointed at it and the rest are
// soft-deleted.
//
// Scoped to `entry_type = 'unofficial_whatsapp'` throughout. No other channel
// could produce these rows, and a repair that ranged wider is one nobody can
// reason about.
func mergeSplitGroupConversations(tx *gorm.DB) error {
	if !tx.Migrator().HasTable("unofficial_whatsapp_conversations") ||
		!tx.Migrator().HasColumn("unofficial_whatsapp_conversations", "chat_id") {
		return nil
	}

	// One consistent mapping of superseded conversation -> survivor, reused by
	// every repoint below so the tables cannot disagree about which row won.
	// Both columns are uuid, matching the conversation table's own id type.
	const duplicatesCTE = `
		SELECT id AS duplicate_id, survivor_id
		FROM (
			SELECT id,
			       FIRST_VALUE(id) OVER (
			           PARTITION BY instance_id, chat_id
			           ORDER BY created_at ASC, id ASC
			       ) AS survivor_id
			FROM unofficial_whatsapp_conversations
			WHERE deleted_at IS NULL
		) ranked
		WHERE id <> survivor_id
	`

	var duplicateCount int64
	if err := tx.Raw(`SELECT COUNT(*) FROM (` + duplicatesCTE + `) d`).
		Scan(&duplicateCount).Error; err != nil {
		return err
	}
	if duplicateCount == 0 {
		// The overwhelmingly common case, including every fresh database. Doing
		// the count first keeps the steady-state cost of this repair at one
		// cheap query per boot instead of a dozen no-op UPDATEs.
		return repointGroupSubjects(tx)
	}

	// Append-only histories: several rows per entry is their normal state, so a
	// merge simply lengthens the list and nothing can collide.
	appendOnly := []entryTable{
		{name: "conversation_messages"},
		{name: "conversation_media"},
		{name: "conversation_events"},
		{name: "analyses"},
		{name: "assignment_history"},
		{name: "workflow_runs"},
		{name: "ai_attendance_sessions", idIsText: true},
	}
	for _, t := range appendOnly {
		if !tx.Migrator().HasTable(t.name) {
			continue
		}
		sql := fmt.Sprintf(`
			UPDATE %s AS t
			SET entry_id = %s
			FROM (%s) AS d
			WHERE %s = %s
			  AND t.entry_type = 'unofficial_whatsapp'
		`, t.name, t.idExpr("d.survivor_id"), duplicatesCTE,
			"t.entry_id", t.idExpr("d.duplicate_id"))
		if err := tx.Exec(sql).Error; err != nil {
			return fmt.Errorf("repointing %s: %w", t.name, err)
		}
	}

	// At-most-one-per-entry tables. Each carries a single logical value the UI
	// renders as one thing — the assignment, the current stage, this label,
	// this opportunity link — so a second row is not a longer history, it is a
	// contradiction. Move what the survivor lacks; delete what it already has.
	guarded := []entryTable{
		{name: "inbox_assignments"},
		{name: "entry_stages", dedupeOn: []string{"stage_id"}, softDeleted: true},
		{name: "entry_labels", dedupeOn: []string{"label_id"}, softDeleted: true},
		{name: "opportunity_conversations", dedupeOn: []string{"opportunity_id"}},
	}
	for _, t := range guarded {
		if !tx.Migrator().HasTable(t.name) {
			continue
		}

		match := "existing.entry_id = d.survivor_id AND existing.entry_type = 'unofficial_whatsapp'"
		for _, col := range t.dedupeOn {
			match += fmt.Sprintf(" AND existing.%s = t.%s", col, col)
		}
		if t.softDeleted {
			match += " AND existing.deleted_at IS NULL"
		}

		move := fmt.Sprintf(`
			UPDATE %[1]s AS t
			SET entry_id = d.survivor_id
			FROM (%[2]s) AS d
			WHERE t.entry_id = d.duplicate_id
			  AND t.entry_type = 'unofficial_whatsapp'
			  AND NOT EXISTS (SELECT 1 FROM %[1]s AS existing WHERE %[3]s)
		`, t.name, duplicatesCTE, match)
		if err := tx.Exec(move).Error; err != nil {
			return fmt.Errorf("repointing %s: %w", t.name, err)
		}

		// Whatever could not move is deleted rather than left pointing at a
		// conversation that is about to disappear: a row keyed to a soft-deleted
		// entry is invisible to every read path and immortal.
		drop := fmt.Sprintf(`
			DELETE FROM %s AS t
			USING (%s) AS d
			WHERE t.entry_id = d.duplicate_id
			  AND t.entry_type = 'unofficial_whatsapp'
		`, t.name, duplicatesCTE)
		if err := tx.Exec(drop).Error; err != nil {
			return fmt.Errorf("clearing leftover %s: %w", t.name, err)
		}
	}

	// The duplicate conversations themselves. Soft-deleted rather than dropped:
	// this is a live database, and a merge that folded something it should not
	// have is only recoverable while the rows still exist.
	soft := fmt.Sprintf(`
		UPDATE unofficial_whatsapp_conversations AS c
		SET deleted_at = NOW()
		FROM (%s) AS d
		WHERE c.id = d.duplicate_id
	`, duplicatesCTE)
	if err := tx.Exec(soft).Error; err != nil {
		return fmt.Errorf("soft-deleting merged conversations: %w", err)
	}

	return repointGroupSubjects(tx)
}

// repointGroupSubjects makes each surviving group conversation point at the
// GROUP as its subject instead of at whichever participant happened to speak
// first.
//
// Only where a group subject contact already exists. The webhook path creates
// one on the next inbound message, and inventing a row here would duplicate that
// work and race it.
func repointGroupSubjects(tx *gorm.DB) error {
	if !tx.Migrator().HasColumn("unofficial_whatsapp_contacts", "is_group") {
		return nil
	}
	return tx.Exec(`
		UPDATE unofficial_whatsapp_conversations AS c
		SET contact_id = g.id
		FROM unofficial_whatsapp_contacts AS g
		WHERE c.is_group = true
		  AND c.deleted_at IS NULL
		  AND g.instance_id = c.instance_id
		  AND g.jid = c.chat_id
		  AND g.deleted_at IS NULL
		  AND c.contact_id <> g.id
	`).Error
}
