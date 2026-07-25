package invoice_repository

import (
	"encoding/json"
	"time"
	"vozko/domain/invoice"
	"vozko/infra/database/schema"

	"gorm.io/gorm"
)

type repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) invoice.Repository {
	return &repository{db: db}
}

func (r *repository) Create(inv *invoice.Invoice) error {
	model := toSchema(inv)
	return r.db.Create(&model).Error
}

func (r *repository) GetByID(id string) (*invoice.Invoice, error) {
	var model schema.Invoice
	if err := r.db.Where("id = ?", id).First(&model).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, invoice.ErrInvoiceNotFound
		}
		return nil, err
	}
	return toDomain(&model), nil
}

func (r *repository) GetByExternalID(externalID string) (*invoice.Invoice, error) {
	var model schema.Invoice
	if err := r.db.Where("external_id = ?", externalID).First(&model).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return toDomain(&model), nil
}

func (r *repository) GetByIdempotencyKey(key string) (*invoice.Invoice, error) {
	if key == "" {
		return nil, nil
	}
	var model schema.Invoice
	if err := r.db.Where("idempotency_key = ?", key).First(&model).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return toDomain(&model), nil
}

func (r *repository) ListUnpaidByPurpose(purpose invoice.Purpose, afterID string, limit int) ([]invoice.Invoice, error) {
	if limit <= 0 {
		limit = 500
	}
	q := r.db.Where("purpose = ? AND status IN ?",
		string(purpose.Normalize()),
		[]string{string(invoice.StatusPending), string(invoice.StatusOverdue)})
	if afterID != "" {
		q = q.Where("id > ?", afterID)
	}
	var models []schema.Invoice
	if err := q.Order("id ASC").Limit(limit).Find(&models).Error; err != nil {
		return nil, err
	}
	out := make([]invoice.Invoice, len(models))
	for i := range models {
		out[i] = *toDomain(&models[i])
	}
	return out, nil
}

func (r *repository) UpdateStatus(id string, status invoice.Status) error {
	return r.db.Model(&schema.Invoice{}).Where("id = ?", id).Update("status", string(status)).Error
}

func (r *repository) MarkPaid(id string, amountUSD int64) (bool, error) {
	now := time.Now().Unix()
	result := r.db.Model(&schema.Invoice{}).Where("id = ? AND status != ?", id, string(invoice.StatusPaid)).Updates(map[string]interface{}{
		"status":     string(invoice.StatusPaid),
		"amount_usd": amountUSD,
		"paid_at":    now,
	})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func (r *repository) ListByWorkspace(workspaceID string, page, pageSize int) ([]invoice.Invoice, int64, error) {
	var total int64
	r.db.Model(&schema.Invoice{}).Where("workspace_id = ?", workspaceID).Count(&total)

	var models []schema.Invoice
	offset := (page - 1) * pageSize
	if err := r.db.Where("workspace_id = ?", workspaceID).
		Order("created_at DESC").
		Offset(offset).Limit(pageSize).
		Find(&models).Error; err != nil {
		return nil, 0, err
	}

	invoices := make([]invoice.Invoice, len(models))
	for i, m := range models {
		invoices[i] = *toDomain(&m)
	}
	return invoices, total, nil
}

func toSchema(inv *invoice.Invoice) *schema.Invoice {
	lineItems := "[]"
	if len(inv.LineItems) > 0 {
		if b, err := json.Marshal(inv.LineItems); err == nil {
			lineItems = string(b)
		}
	}
	return &schema.Invoice{
		ID:               inv.ID,
		WorkspaceID:      inv.WorkspaceID,
		UserID:           inv.UserID,
		Purpose:          string(inv.NormalizedPurpose()),
		PlanDefinitionID: inv.PlanDefinitionID,
		AmountBRL:        inv.AmountBRL,
		AmountUSD:        inv.AmountUSD,
		ExchangeRate:     inv.ExchangeRate,
		Status:           string(inv.Status),
		BillingType:      inv.BillingType,
		BillingCycle:     inv.BillingCycle,
		ExternalID:       inv.ExternalID,
		IdempotencyKey:   inv.IdempotencyKey,
		CreditableUSD:    inv.CreditableUSD,
		PixQrCode:        inv.PixQrCode,
		PixCopy:          inv.PixCopy,
		BankSlipUrl:      inv.BankSlipUrl,
		InvoiceUrl:       inv.InvoiceUrl,
		PaidAt:           inv.PaidAt,
		Description:      inv.Description,
		DueDate:          inv.DueDate,
		LineItemsJSON:    lineItems,
	}
}

func toDomain(m *schema.Invoice) *invoice.Invoice {
	var lineItems []invoice.InvoiceLineItem
	if m.LineItemsJSON != "" && m.LineItemsJSON != "[]" {
		_ = json.Unmarshal([]byte(m.LineItemsJSON), &lineItems)
	}
	return &invoice.Invoice{
		ID:               m.ID,
		WorkspaceID:      m.WorkspaceID,
		UserID:           m.UserID,
		Purpose:          invoice.Purpose(m.Purpose),
		PlanDefinitionID: m.PlanDefinitionID,
		AmountBRL:        m.AmountBRL,
		AmountUSD:        m.AmountUSD,
		ExchangeRate:     m.ExchangeRate,
		Status:           invoice.Status(m.Status),
		BillingType:      m.BillingType,
		BillingCycle:     m.BillingCycle,
		ExternalID:       m.ExternalID,
		IdempotencyKey:   m.IdempotencyKey,
		CreditableUSD:    m.CreditableUSD,
		PixQrCode:        m.PixQrCode,
		PixCopy:          m.PixCopy,
		BankSlipUrl:      m.BankSlipUrl,
		InvoiceUrl:       m.InvoiceUrl,
		PaidAt:           m.PaidAt,
		Description:      m.Description,
		DueDate:          m.DueDate,
		LineItems:        lineItems,
		CreatedAt:        m.CreatedAt,
		UpdatedAt:        m.UpdatedAt,
	}
}
