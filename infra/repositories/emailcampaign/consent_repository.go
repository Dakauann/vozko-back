package emailcampaign

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"vozko/domain/emailaddress"
	"vozko/domain/emailconsent"
	"vozko/infra/database"
	"vozko/infra/database/schema"
)

const addressChunk = 1000

type SubscriptionRepository struct {
	db  *gorm.DB
	now func() time.Time
}

func NewSubscriptionRepository(db *gorm.DB) *SubscriptionRepository {
	return &SubscriptionRepository{db: db, now: func() time.Time { return time.Now().UTC() }}
}

func subscriptionRow(s emailconsent.Subscription, now time.Time) schema.EmailSubscription {
	id := s.ID
	if id == "" {
		id = uuid.NewString()
	}
	created := s.CreatedAt
	if created.IsZero() {
		created = now
	}
	var consented *time.Time
	if !s.ConsentedAt.IsZero() {
		at := s.ConsentedAt.UTC()
		consented = &at
	}
	return schema.EmailSubscription{
		ID: id, WorkspaceID: s.WorkspaceID, Address: string(s.Address), LeadID: schema.OptionalText(s.LeadID),
		Status: string(s.Status), Source: string(s.Source), ConsentedAt: consented, ConsentIP: s.ConsentIP,
		UnsubscribedAt: utc(s.UnsubscribedAt), UnsubscribeSource: string(s.UnsubscribeSource), CreatedAt: created, UpdatedAt: now,
	}
}

func subscriptionOf(row schema.EmailSubscription) emailconsent.Subscription {
	s := emailconsent.Subscription{
		ID: row.ID, WorkspaceID: row.WorkspaceID, Address: emailaddress.Address(row.Address), LeadID: string(row.LeadID),
		Status: emailconsent.Status(row.Status), Source: emailconsent.Source(row.Source), ConsentIP: row.ConsentIP,
		UnsubscribedAt: utc(row.UnsubscribedAt), UnsubscribeSource: emailconsent.UnsubscribeSource(row.UnsubscribeSource),
		CreatedAt: row.CreatedAt.UTC(), UpdatedAt: row.UpdatedAt.UTC(),
	}
	if row.ConsentedAt != nil {
		s.ConsentedAt = row.ConsentedAt.UTC()
	}
	return s
}

var subscriptionUpsert = clause.OnConflict{
	Columns:   []clause.Column{{Name: "workspace_id"}, {Name: "address"}},
	DoUpdates: clause.AssignmentColumns([]string{"lead_id", "status", "source", "consented_at", "consent_ip", "unsubscribed_at", "unsubscribe_source", "updated_at"}),
}

func (r *SubscriptionRepository) Get(ctx context.Context, workspaceID string, address emailaddress.Address) (*emailconsent.Subscription, error) {
	var row schema.EmailSubscription
	if err := r.db.WithContext(ctx).Where("workspace_id = ? AND address = ?", workspaceID, string(address)).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, emailconsent.ErrSubscriptionNotFound
		}
		return nil, err
	}
	s := subscriptionOf(row)
	return &s, nil
}

func (r *SubscriptionRepository) Find(ctx context.Context, workspaceID string, addresses []emailaddress.Address) (map[emailaddress.Address]emailconsent.Subscription, error) {
	out := make(map[emailaddress.Address]emailconsent.Subscription, len(addresses))
	if len(addresses) == 0 {
		return out, nil
	}
	var rows []schema.EmailSubscription
	if err := r.db.WithContext(ctx).Where("workspace_id = ? AND address = ANY(?)", workspaceID, database.Texts(addressStrings(addresses))).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[emailaddress.Address(row.Address)] = subscriptionOf(row)
	}
	return out, nil
}

func (r *SubscriptionRepository) Save(ctx context.Context, sub emailconsent.Subscription) (emailconsent.Subscription, error) {
	row := subscriptionRow(sub, r.now())
	if err := r.db.WithContext(ctx).Clauses(subscriptionUpsert).Create(&row).Error; err != nil {
		return emailconsent.Subscription{}, err
	}
	stored, err := r.Get(ctx, sub.WorkspaceID, sub.Address)
	if err != nil {
		return emailconsent.Subscription{}, err
	}
	return *stored, nil
}

func (r *SubscriptionRepository) SaveMany(ctx context.Context, subs []emailconsent.Subscription) error {
	if len(subs) == 0 {
		return nil
	}
	now := r.now()
	rows := make([]schema.EmailSubscription, len(subs))
	for i, s := range subs {
		rows[i] = subscriptionRow(s, now)
	}
	return r.db.WithContext(ctx).Clauses(subscriptionUpsert).CreateInBatches(rows, addressChunk).Error
}

func addressStrings(addresses []emailaddress.Address) []string {
	out := make([]string, len(addresses))
	for i, a := range addresses {
		out[i] = string(a)
	}
	return out
}

var _ emailconsent.Repository = (*SubscriptionRepository)(nil)
