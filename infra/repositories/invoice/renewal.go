package invoice_repository

import (
	"errors"
	"time"

	"vozko/domain/invoice"
	"vozko/domain/payment"
	"vozko/infra/database/schema"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type chargeRenewals struct {
	db *gorm.DB
}

func NewChargeRenewals(db *gorm.DB) invoice.ChargeRenewals {
	return &chargeRenewals{db: db}
}

func awaitingPaymentStatuses() []string {
	return []string{string(invoice.StatusPending), string(invoice.StatusOverdue)}
}

func (r *chargeRenewals) BeginRenewal(invoiceID string, previous invoice.SupersededCharge, leaseUntil, now time.Time) (bool, error) {
	acquired := false
	err := r.db.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&schema.Invoice{}).
			Where("id = ? AND external_id = ? AND status IN ? AND (renewal_lease_until IS NULL OR renewal_lease_until < ?)",
				invoiceID, previous.ExternalID, awaitingPaymentStatuses(), now).
			Update("renewal_lease_until", leaseUntil)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}
		acquired = true
		return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&schema.InvoiceSupersededCharge{
			InvoiceID:  invoiceID,
			Provider:   string(previous.Provider),
			ExternalID: previous.ExternalID,
		}).Error
	})
	if err != nil {
		return false, err
	}
	return acquired, nil
}

func (r *chargeRenewals) CompleteRenewal(invoiceID, previousExternalID string, charge invoice.ChargeAttachment) (bool, error) {
	result := r.db.Model(&schema.Invoice{}).
		Where("id = ? AND external_id = ? AND status IN ?", invoiceID, previousExternalID, awaitingPaymentStatuses()).
		Updates(map[string]interface{}{
			"external_id":         charge.ExternalID,
			"pix_qr_code":         charge.PixQrCode,
			"pix_copy":            charge.PixCopy,
			"bank_slip_url":       charge.BankSlipUrl,
			"invoice_url":         charge.InvoiceUrl,
			"charge_expires_at":   charge.ExpiresAt,
			"renewal_lease_until": nil,
		})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func (r *chargeRenewals) AbortRenewal(invoiceID string, previous invoice.SupersededCharge, previousStillPayable bool) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&schema.Invoice{}).Where("id = ?", invoiceID).Update("renewal_lease_until", nil).Error; err != nil {
			return err
		}
		if !previousStillPayable {
			return nil
		}
		return tx.Where("invoice_id = ? AND provider = ? AND external_id = ?", invoiceID, string(previous.Provider), previous.ExternalID).
			Delete(&schema.InvoiceSupersededCharge{}).Error
	})
}

func (r *repository) GetBySupersededCharge(provider payment.Provider, externalID string) (*invoice.Invoice, error) {
	if externalID == "" {
		return nil, nil
	}
	superseded := r.db.Model(&schema.InvoiceSupersededCharge{}).Select("invoice_id").Where("external_id = ?", externalID)
	if provider != "" {
		superseded = superseded.Where("provider = ?", string(provider))
	}
	var model schema.Invoice
	if err := r.db.Where("id IN (?)", superseded).First(&model).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return toDomain(&model), nil
}
