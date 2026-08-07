package unofficial_whatsapp_repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	uw "vozko/domain/unofficial_whatsapp"
	"vozko/infra/database/schema"
)

type contactRepository struct {
	db *gorm.DB
}

// NewContactRepository builds the contact repository.
func NewContactRepository(db *gorm.DB) uw.ContactRepository {
	return &contactRepository{db: db}
}

// FindOrCreate resolves a conversation subject and reconciles WhatsApp's two
// identifier forms.
//
// The same human reaches us under a phone-number JID and under a LID, and which
// one arrives depends on privacy settings and on whether the number is saved.
// Creating a second row for the second form would split one real chat into two
// CRM conversations, which reads to an operator as data loss rather than as a
// bug — so a lookup that misses on the JID falls back to the LID (and vice
// versa) and backfills whichever identifier was missing.
//
// A group subject takes the same path with none of the reconciliation: a group
// has exactly one identifier, no phone number and no LID, so it resolves by JID
// alone.
func (r *contactRepository) FindOrCreate(ctx context.Context, in uw.FindOrCreateContactInput) (*uw.Contact, error) {
	jid := strings.TrimSpace(in.JID)
	lid := strings.TrimSpace(in.LID)
	isGroup := in.IsGroup || uw.IsGroupJID(jid)

	// A group has no number. PhoneFromJID already refuses to derive one from a
	// group id, and the explicit zeroing here says so at the boundary rather
	// than relying on that: this column is what the dialer, the lead bridge and
	// broadcast targeting read, and a group id in it is addressable nonsense.
	phone := ""
	if !isGroup {
		phone = uw.NormalizePhone(in.PhoneNumber)
		if phone == "" {
			phone = uw.PhoneFromJID(jid)
		}
	}

	if existing, err := r.resolveExisting(ctx, in.InstanceID, jid, lid, phone); err != nil {
		return nil, err
	} else if existing != nil {
		return r.backfillIdentity(ctx, existing, jid, lid, phone, in.Name)
	}

	record := &schema.UnofficialWhatsAppContact{
		WorkspaceID: in.WorkspaceID,
		InstanceID:  in.InstanceID,
		JID:         jid,
		LID:         lid,
		IsGroup:     isGroup,
		PhoneNumber: phone,
		Name:        truncate(strings.TrimSpace(in.Name), 255),
	}
	if err := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "instance_id"}, {Name: "jid"}},
			// Partial unique index: the predicate must be repeated or Postgres
			// refuses to use it as the conflict arbiter (42P10).
			TargetWhere: clause.Where{
				Exprs: []clause.Expression{clause.Expr{SQL: "deleted_at IS NULL"}},
			},
			DoNothing: true,
		}).
		Create(record).Error; err != nil {
		return nil, err
	}
	return r.FindByJID(ctx, in.InstanceID, jid)
}

// resolveExisting looks the contact up by each identifier we hold, in
// descending order of trust: the JID identifies a chat, the LID identifies a
// person, and the phone number is the CRM's own key.
func (r *contactRepository) resolveExisting(ctx context.Context, instanceID, jid, lid, phone string) (*uw.Contact, error) {
	lookups := []struct{ column, value string }{
		{"jid", jid},
		{"lid", lid},
		{"phone_number", phone},
	}
	for _, lookup := range lookups {
		if lookup.value == "" {
			continue
		}
		var record schema.UnofficialWhatsAppContact
		err := r.db.WithContext(ctx).
			Where("instance_id = ? AND "+lookup.column+" = ?", instanceID, lookup.value).
			First(&record).Error
		if err == nil {
			return toContactDomain(&record), nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}
	return nil, nil
}

// backfillIdentity fills in identifiers the stored row was missing.
//
// It never OVERWRITES a stored identifier with a different one: that would mean
// two people share a row, and silently repointing it would attach one person's
// transcript to the other.
func (r *contactRepository) backfillIdentity(
	ctx context.Context,
	contact *uw.Contact,
	jid, lid, phone, name string,
) (*uw.Contact, error) {
	update := map[string]any{}
	if contact.JID == "" && jid != "" {
		update["jid"] = jid
		contact.JID = jid
	}
	if contact.LID == "" && lid != "" {
		update["lid"] = lid
		contact.LID = lid
	}
	if contact.PhoneNumber == "" && phone != "" {
		update["phone_number"] = phone
		contact.PhoneNumber = phone
	}
	if contact.Name == "" && strings.TrimSpace(name) != "" {
		update["name"] = truncate(strings.TrimSpace(name), 255)
		contact.Name = update["name"].(string)
	}
	if len(update) == 0 {
		return contact, nil
	}

	if err := r.db.WithContext(ctx).Model(&schema.UnofficialWhatsAppContact{}).
		Where("id = ?", contact.ID).
		Updates(update).Error; err != nil {
		// A conflict here means another row already owns that identifier, i.e.
		// the merge we were about to perform is not safe. Keep what we have
		// rather than failing the inbound message: a slightly under-populated
		// contact is recoverable, a dropped message is not.
		if isUniqueViolation(err) {
			return contact, nil
		}
		return nil, err
	}
	return contact, nil
}

func (r *contactRepository) FindByID(ctx context.Context, id string) (*uw.Contact, error) {
	var record schema.UnofficialWhatsAppContact
	if err := r.db.WithContext(ctx).First(&record, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, uw.ErrContactNotFound
		}
		return nil, err
	}
	return toContactDomain(&record), nil
}

// FindByIDs batch-loads one page of inbox senders. A per-row lookup here would
// make the inbox N+1 on every render.
func (r *contactRepository) FindByIDs(ctx context.Context, ids []string) ([]*uw.Contact, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var records []schema.UnofficialWhatsAppContact
	if err := r.db.WithContext(ctx).Where("id IN ?", ids).Find(&records).Error; err != nil {
		return nil, err
	}
	out := make([]*uw.Contact, 0, len(records))
	for i := range records {
		out = append(out, toContactDomain(&records[i]))
	}
	return out, nil
}

func (r *contactRepository) FindByJID(ctx context.Context, instanceID, jid string) (*uw.Contact, error) {
	var record schema.UnofficialWhatsAppContact
	err := r.db.WithContext(ctx).
		First(&record, "instance_id = ? AND jid = ?", instanceID, jid).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, uw.ErrContactNotFound
		}
		return nil, err
	}
	return toContactDomain(&record), nil
}

// UpdateProfile refreshes the enrichable fields.
//
// Empty values are skipped rather than written, ALL of them: a profile read that
// came back partial must not blank what an earlier one resolved, which is how a
// named contact turns back into a bare number.
//
// That rule used to hold for the strings only. `is_business` and
// `profile_fetched_at` were written unconditionally, so every caller that
// updated one field reset both — a contacts-webhook rename demoted a verified
// business account to an ordinary one, and any caller that did not mean to
// advance the staleness clock advanced it anyway.
func (r *contactRepository) UpdateProfile(ctx context.Context, id string, p uw.ContactProfile) error {
	update := map[string]any{}
	// A false here means "this read did not establish it", never "this account
	// stopped being a business".
	if p.IsBusiness {
		update["is_business"] = true
	}
	// The zero time means "not a profile read" — the caller touched one field
	// and must not consume the subject's refresh budget for a week.
	if !p.FetchedAt.IsZero() {
		update["profile_fetched_at"] = p.FetchedAt
	}
	setIfPresent(update, "name", truncate(p.Name, 255))
	setIfPresent(update, "contact_name", truncate(p.ContactName, 255))
	setIfPresent(update, "verified_name", truncate(p.VerifiedName, 255))
	setIfPresent(update, "picture_url", truncate(p.PictureURL, 1024))
	setIfPresent(update, "picture_source_url", truncate(p.PictureSourceURL, 1024))

	if len(update) == 0 {
		return nil
	}

	result := r.db.WithContext(ctx).Model(&schema.UnofficialWhatsAppContact{}).
		Where("id = ?", id).
		Updates(update)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return uw.ErrContactNotFound
	}
	return nil
}

func (r *contactRepository) SetBlocked(ctx context.Context, id string, blocked bool, at time.Time) error {
	update := map[string]any{"blocked": blocked, "blocked_at": nil}
	if blocked {
		update["blocked_at"] = at
	}
	result := r.db.WithContext(ctx).Model(&schema.UnofficialWhatsAppContact{}).
		Where("id = ?", id).
		Updates(update)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return uw.ErrContactNotFound
	}
	return nil
}

// LinkLead attaches the CRM lead this contact resolved to.
//
// Written once, never repointed: a contact whose lead changed under it would
// move its whole transcript to a different person in every CRM view.
func (r *contactRepository) LinkLead(ctx context.Context, id, leadID string) error {
	return r.db.WithContext(ctx).Model(&schema.UnofficialWhatsAppContact{}).
		Where("id = ? AND lead_id IS NULL", id).
		Update("lead_id", leadID).Error
}
