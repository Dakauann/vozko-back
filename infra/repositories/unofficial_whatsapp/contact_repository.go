package unofficial_whatsapp_repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	uw "vozko/domain/unofficial_whatsapp"
	"vozko/infra/database"
	"vozko/infra/database/schema"
)

type contactRepository struct {
	db *gorm.DB
}

func NewContactRepository(db *gorm.DB) uw.ContactRepository {
	return &contactRepository{db: db}
}

func (r *contactRepository) FindOrCreate(ctx context.Context, in uw.FindOrCreateContactInput) (*uw.Contact, error) {
	jid := strings.TrimSpace(in.JID)
	lid := strings.TrimSpace(in.LID)
	isGroup := in.IsGroup || uw.IsGroupJID(jid)

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
		if database.IsUniqueViolation(err) {
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

func (r *contactRepository) FindByIDs(ctx context.Context, ids []string) ([]*uw.Contact, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var records []schema.UnofficialWhatsAppContact
	if err := r.db.WithContext(ctx).
		Where("id IN ? OR lead_id IN ?", ids, ids).
		Find(&records).Error; err != nil {
		return nil, err
	}
	out := make([]*uw.Contact, 0, len(records))
	for i := range records {
		out = append(out, toContactDomain(&records[i]))
	}
	return out, nil
}

func (r *contactRepository) FindByHandles(
	ctx context.Context,
	instanceID string,
	handles []string,
) ([]*uw.Contact, error) {
	if instanceID == "" || len(handles) == 0 {
		return nil, nil
	}

	phones := make([]string, 0, len(handles))
	jids := make([]string, 0, len(handles))
	for _, handle := range handles {
		handle = strings.TrimSpace(handle)
		if handle == "" {
			continue
		}
		if digits := strings.TrimPrefix(handle, "+"); digits != handle {
			phones = append(phones, digits)
			continue
		}
		jids = append(jids, handle)
	}
	if len(phones) == 0 && len(jids) == 0 {
		return nil, nil
	}

	query := r.db.WithContext(ctx).Where("instance_id = ?", instanceID)
	switch {
	case len(phones) > 0 && len(jids) > 0:
		query = query.Where("phone_number IN ? OR jid IN ?", phones, jids)
	case len(phones) > 0:
		query = query.Where("phone_number IN ?", phones)
	default:
		query = query.Where("jid IN ?", jids)
	}

	var records []schema.UnofficialWhatsAppContact
	if err := query.Find(&records).Error; err != nil {
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

func (r *contactRepository) UpdateProfile(ctx context.Context, id string, p uw.ContactProfile) error {
	update := map[string]any{}
	if p.IsBusiness {
		update["is_business"] = true
	}
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

func (r *contactRepository) LinkLead(ctx context.Context, id, leadID string) error {
	return r.db.WithContext(ctx).Model(&schema.UnofficialWhatsAppContact{}).
		Where("id = ? AND lead_id IS NULL", id).
		Update("lead_id", leadID).Error
}
