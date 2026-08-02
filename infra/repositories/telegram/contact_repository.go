package telegram_repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	tgdomain "vozko/domain/telegram"
	"vozko/infra/database/schema"
)

type contactRepository struct {
	db *gorm.DB
}

// NewContactRepository builds the Telegram contact repository.
func NewContactRepository(db *gorm.DB) tgdomain.ContactRepository {
	return &contactRepository{db: db}
}

// FindOrCreate resolves a contact by (account, telegram user id).
//
// The profile fields the update already carried are written on creation, so a
// first message yields a named contact with no extra API call — Telegram puts
// first_name, username and language_code straight in the payload, unlike Meta.
func (r *contactRepository) FindOrCreate(ctx context.Context, in tgdomain.FindOrCreateContactInput) (*tgdomain.Contact, error) {
	existing, err := r.FindByTGUserID(ctx, in.AccountID, in.TGUserID)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, tgdomain.ErrContactNotFound) {
		return nil, err
	}

	chatType := in.ChatType
	if chatType == "" {
		chatType = tgdomain.ChatTypePrivate
	}
	chatID := in.TGChatID
	if chatID == 0 {
		// For a private chat the chat id equals the user id; falling back keeps
		// the row usable even if a payload omitted it.
		chatID = in.TGUserID
	}

	record := &schema.TelegramContact{
		WorkspaceID:  in.WorkspaceID,
		AccountID:    in.AccountID,
		TGUserID:     in.TGUserID,
		TGChatID:     chatID,
		ChatType:     chatType,
		Username:     strings.TrimPrefix(strings.TrimSpace(in.Username), "@"),
		FirstName:    strings.TrimSpace(in.FirstName),
		LastName:     strings.TrimSpace(in.LastName),
		LanguageCode: strings.TrimSpace(in.LanguageCode),
		IsPremium:    in.IsPremium,
	}

	if err := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "account_id"}, {Name: "tg_user_id"}},
			// The unique index is PARTIAL (WHERE deleted_at IS NULL): a
			// soft-deleted row must not block re-creating the contact. Postgres
			// will not infer a partial index as the conflict arbiter unless the
			// predicate is repeated here — a bare ON CONFLICT (cols) fails with
			// 42P10.
			TargetWhere: clause.Where{
				Exprs: []clause.Expression{clause.Expr{SQL: "deleted_at IS NULL"}},
			},
			DoNothing: true,
		}).
		Create(record).Error; err != nil {
		return nil, err
	}
	return r.FindByTGUserID(ctx, in.AccountID, in.TGUserID)
}

func (r *contactRepository) FindByID(ctx context.Context, id string) (*tgdomain.Contact, error) {
	var record schema.TelegramContact
	if err := r.db.WithContext(ctx).First(&record, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, tgdomain.ErrContactNotFound
		}
		return nil, err
	}
	return toContactDomain(&record), nil
}

// FindByIDs batch-loads one page of senders. The inbox hydrates a whole page
// with this single query; a per-row lookup would make the inbox N+1.
func (r *contactRepository) FindByIDs(ctx context.Context, ids []string) ([]*tgdomain.Contact, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var records []schema.TelegramContact
	if err := r.db.WithContext(ctx).Where("id IN ?", ids).Find(&records).Error; err != nil {
		return nil, err
	}
	out := make([]*tgdomain.Contact, 0, len(records))
	for i := range records {
		out = append(out, toContactDomain(&records[i]))
	}
	return out, nil
}

func (r *contactRepository) FindByTGUserID(ctx context.Context, accountID string, tgUserID int64) (*tgdomain.Contact, error) {
	var record schema.TelegramContact
	if err := r.db.WithContext(ctx).
		First(&record, "account_id = ? AND tg_user_id = ?", accountID, tgUserID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, tgdomain.ErrContactNotFound
		}
		return nil, err
	}
	return toContactDomain(&record), nil
}

func (r *contactRepository) UpdateProfile(ctx context.Context, id string, p tgdomain.ContactProfile) error {
	update := map[string]any{
		"profile_fetched_at": p.FetchedAt,
	}
	// Only non-empty values overwrite. Telegram omits a field the user cleared
	// rather than sending it empty, so blind assignment would erase a known name
	// the moment a later payload happened not to carry it.
	if v := strings.TrimPrefix(strings.TrimSpace(p.Username), "@"); v != "" {
		update["username"] = v
	}
	if v := strings.TrimSpace(p.FirstName); v != "" {
		update["first_name"] = v
	}
	if v := strings.TrimSpace(p.LastName); v != "" {
		update["last_name"] = v
	}
	if v := strings.TrimSpace(p.LanguageCode); v != "" {
		update["language_code"] = v
	}
	if p.PhotoFileID != "" {
		update["photo_file_id"] = p.PhotoFileID
	}
	if p.PhotoURL != "" {
		update["photo_url"] = p.PhotoURL
	}
	update["is_premium"] = p.IsPremium

	result := r.db.WithContext(ctx).Model(&schema.TelegramContact{}).
		Where("id = ?", id).Updates(update)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return tgdomain.ErrContactNotFound
	}
	return nil
}

// SetBlocked records a block/unblock from my_chat_member. In bot mode this is
// the outbound gate: there is no messaging window, only "can we still reach
// them".
func (r *contactRepository) SetBlocked(ctx context.Context, id string, blocked bool, at time.Time) error {
	update := map[string]any{"blocked": blocked}
	if blocked {
		update["blocked_at"] = at
	} else {
		update["blocked_at"] = nil
	}
	result := r.db.WithContext(ctx).Model(&schema.TelegramContact{}).
		Where("id = ?", id).Updates(update)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return tgdomain.ErrContactNotFound
	}
	return nil
}

func (r *contactRepository) SetPhone(ctx context.Context, id, phone string, leadID *string, at time.Time) error {
	update := map[string]any{
		"phone_number":    phone,
		"phone_shared_at": at,
	}
	// The lead link is only ever set, never cleared: a later share that fails to
	// match must not unlink a contact an operator already merged.
	if leadID != nil && *leadID != "" {
		update["lead_id"] = *leadID
	}
	result := r.db.WithContext(ctx).Model(&schema.TelegramContact{}).
		Where("id = ?", id).Updates(update)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return tgdomain.ErrContactNotFound
	}
	return nil
}

// UpdateChatID rewrites the chat id after a group→supergroup migration, which
// Telegram announces as ResponseParameters.migrate_to_chat_id on the failed
// send. Without this the conversation is unreachable from then on.
func (r *contactRepository) UpdateChatID(ctx context.Context, id string, chatID int64) error {
	result := r.db.WithContext(ctx).Model(&schema.TelegramContact{}).
		Where("id = ?", id).Update("tg_chat_id", chatID)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return tgdomain.ErrContactNotFound
	}
	return nil
}

func toContactDomain(record *schema.TelegramContact) *tgdomain.Contact {
	return &tgdomain.Contact{
		ID:               record.ID,
		WorkspaceID:      record.WorkspaceID,
		AccountID:        record.AccountID,
		TGUserID:         record.TGUserID,
		TGChatID:         record.TGChatID,
		ChatType:         record.ChatType,
		Username:         record.Username,
		FirstName:        record.FirstName,
		LastName:         record.LastName,
		LanguageCode:     record.LanguageCode,
		IsPremium:        record.IsPremium,
		PhotoFileID:      record.PhotoFileID,
		PhotoURL:         record.PhotoURL,
		ProfileFetchedAt: record.ProfileFetchedAt,
		PhoneNumber:      record.PhoneNumber,
		PhoneSharedAt:    record.PhoneSharedAt,
		LeadID:           record.LeadID,
		Blocked:          record.Blocked,
		BlockedAt:        record.BlockedAt,
		CreatedAt:        record.CreatedAt,
		UpdatedAt:        record.UpdatedAt,
	}
}
