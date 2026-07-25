package payment_repository

import (
	"vozko/domain/payment"
	"vozko/infra/database/schema"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type splitRepository struct {
	db *gorm.DB
}

func NewSplitRepository(db *gorm.DB) payment.PaymentSplitRepository {
	return &splitRepository{db: db}
}

func (r *splitRepository) Create(split *payment.PaymentSplit) error {
	if split.ID == "" {
		split.ID = uuid.New().String()
	}

	dbSplit := mapToSplitSchema(split)
	if err := r.db.Create(dbSplit).Error; err != nil {
		return err
	}

	mapSplitFromSchema(dbSplit, split)
	return nil
}

func (r *splitRepository) GetSuppliers() ([]*payment.PaymentSplit, error) {
	var dbSplits []schema.PaymentSplit

	if err := r.db.Where("type = ?", schema.SplitTypeSupplier).Find(&dbSplits).Error; err != nil {
		return nil, err
	}

	splits := make([]*payment.PaymentSplit, len(dbSplits))
	for i := range dbSplits {
		splits[i] = &payment.PaymentSplit{}
		mapSplitFromSchema(&dbSplits[i], splits[i])
	}
	return splits, nil
}

func (r *splitRepository) Update(split *payment.PaymentSplit) error {
	dbSplit := mapToSplitSchema(split)
	return r.db.Model(&schema.PaymentSplit{}).
		Where("id = ?", split.ID).
		Updates(map[string]interface{}{
			"name":         dbSplit.Name,
			"type":         dbSplit.Type,
			"provider":     dbSplit.Provider,
			"wallet_id":    dbSplit.WalletID,
			"percentage":   dbSplit.Percentage,
			"fixed_amount": dbSplit.FixedAmount,
		}).Error
}

func (r *splitRepository) Delete(id string) error {
	return r.db.Where("id = ?", id).Delete(&schema.PaymentSplit{}).Error
}

func (r *splitRepository) GetByIDs(ids []string) (map[string]*payment.PaymentSplit, error) {
	var dbSplits []schema.PaymentSplit
	if err := r.db.Where("id IN ?", ids).Find(&dbSplits).Error; err != nil {
		return nil, err
	}

	splits := make(map[string]*payment.PaymentSplit)
	for i := range dbSplits {
		split := &payment.PaymentSplit{}
		mapSplitFromSchema(&dbSplits[i], split)
		splits[split.ID] = split
	}
	return splits, nil
}

func (r *splitRepository) GetByType(splitType payment.SplitType) ([]*payment.PaymentSplit, error) {
	var dbSplits []schema.PaymentSplit
	if err := r.db.Where("type = ?", schema.SplitType(splitType)).Find(&dbSplits).Error; err != nil {
		return nil, err
	}

	splits := make([]*payment.PaymentSplit, len(dbSplits))
	for i := range dbSplits {
		splits[i] = &payment.PaymentSplit{}
		mapSplitFromSchema(&dbSplits[i], splits[i])
	}
	return splits, nil
}

func (r *splitRepository) GetByID(id string) (*payment.PaymentSplit, error) {
	var dbSplit schema.PaymentSplit
	if err := r.db.Where("id = ?", id).First(&dbSplit).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, payment.ErrPaymentSplitNotFound
		}
		return nil, err
	}

	split := &payment.PaymentSplit{}
	mapSplitFromSchema(&dbSplit, split)
	return split, nil
}

func (r *splitRepository) List() ([]payment.PaymentSplit, error) {
	var dbSplits []schema.PaymentSplit
	if err := r.db.Order("created_at DESC").Find(&dbSplits).Error; err != nil {
		return nil, err
	}

	result := make([]payment.PaymentSplit, len(dbSplits))
	for i := range dbSplits {
		mapSplitFromSchema(&dbSplits[i], &result[i])
	}
	return result, nil
}

func mapToSplitSchema(split *payment.PaymentSplit) *schema.PaymentSplit {
	return &schema.PaymentSplit{
		ID:          split.ID,
		Name:        split.Name,
		Type:        schema.SplitType(split.Type),
		Provider:    string(split.Provider),
		WalletID:    split.WalletID,
		Percentage:  split.Percentage,
		FixedAmount: split.FixedAmount,
	}
}

func mapSplitFromSchema(dbSplit *schema.PaymentSplit, target *payment.PaymentSplit) {
	target.ID = dbSplit.ID
	target.Name = dbSplit.Name
	target.Type = payment.SplitType(dbSplit.Type)
	target.Provider = payment.SplitProvider(dbSplit.Provider)
	target.WalletID = dbSplit.WalletID
	target.Percentage = dbSplit.Percentage
	target.FixedAmount = dbSplit.FixedAmount
	target.CreatedAt = dbSplit.CreatedAt
	target.UpdatedAt = dbSplit.UpdatedAt
}
