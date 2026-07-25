package insurance_repository

import (
	"context"
	"time"

	"vozko/domain/insurance"
	"vozko/infra/database/schema"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) insurance.InsuranceRepository {
	if db == nil {
		return nil
	}
	return &repository{db: db}
}

func (r *repository) SaveQuotation(ctx context.Context, quotation *insurance.Quotation) error {
	if quotation == nil {
		return nil
	}

	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		quotationRecord := schema.InsuranceQuotation{
			ID:         quotation.ID,
			UserID:     quotation.UserID,
			PolicyType: string(quotation.PolicyType),
			CreatedAt:  quotation.CreatedAt,
		}

		if err := tx.Create(&quotationRecord).Error; err != nil {
			return err
		}

		for _, quote := range quotation.Quotes {
			quoteRecord := mapQuoteToSchema(quote, quotation.ID)
			if err := tx.Create(&quoteRecord).Error; err != nil {
				return err
			}
		}

		return nil
	})
}

func (r *repository) GetQuotationByID(ctx context.Context, quotationID string) (*insurance.Quotation, error) {
	var row schema.InsuranceQuotation
	if err := r.db.WithContext(ctx).
		Preload("Quotes").
		Where("id = ?", quotationID).
		First(&row).Error; err != nil {
		return nil, err
	}

	quotation := mapQuotationFromSchema(row)
	return &quotation, nil
}

func (r *repository) ListQuotationsByUser(ctx context.Context, userID string) ([]insurance.Quotation, error) {
	var rows []schema.InsuranceQuotation
	if err := r.db.WithContext(ctx).
		Preload("Quotes").
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Find(&rows).Error; err != nil {
		return nil, err
	}

	quotations := make([]insurance.Quotation, 0, len(rows))
	for _, row := range rows {
		quotations = append(quotations, mapQuotationFromSchema(row))
	}

	return quotations, nil
}

func mapQuoteToSchema(q insurance.InsuranceQuote, quotationID string) schema.InsuranceQuote {
	metadata := datatypes.JSONMap{}
	if len(q.Metadata) > 0 {
		metadata = make(datatypes.JSONMap, len(q.Metadata))
		for k, v := range q.Metadata {
			metadata[k] = v
		}
	}

	createdAt := q.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now()
	}

	return schema.InsuranceQuote{
		ID:          q.ID,
		QuotationID: quotationID,
		ExternalID:  q.ExternalID,
		UserID:      q.UserID,
		PolicyType:  string(q.PolicyType),
		Provider:    string(q.Provider),
		CoverageAmt: q.CoverageAmt,
		Premium:     q.Premium,
		Currency:    q.Currency,
		ValidUntil:  q.ValidUntil,
		Metadata:    metadata,
		CreatedAt:   createdAt,
		UpdatedAt:   time.Now(),
	}
}

func mapQuoteFromSchema(row schema.InsuranceQuote) insurance.InsuranceQuote {
	metadata := make(map[string]interface{}, len(row.Metadata))
	for k, v := range row.Metadata {
		metadata[k] = v
	}

	return insurance.InsuranceQuote{
		ID:          row.ID,
		QuotationID: row.QuotationID,
		ExternalID:  row.ExternalID,
		UserID:      row.UserID,
		PolicyType:  insurance.PolicyType(row.PolicyType),
		Provider:    insurance.InsuranceProvider(row.Provider),
		CoverageAmt: row.CoverageAmt,
		Premium:     row.Premium,
		Currency:    row.Currency,
		ValidUntil:  row.ValidUntil,
		CreatedAt:   row.CreatedAt,
		Metadata:    metadata,
	}
}

func mapQuotationFromSchema(row schema.InsuranceQuotation) insurance.Quotation {
	quotes := make([]insurance.InsuranceQuote, 0, len(row.Quotes))
	for _, q := range row.Quotes {
		quotes = append(quotes, mapQuoteFromSchema(q))
	}

	return insurance.Quotation{
		ID:         row.ID,
		UserID:     row.UserID,
		PolicyType: insurance.PolicyType(row.PolicyType),
		Quotes:     quotes,
		CreatedAt:  row.CreatedAt,
	}
}
