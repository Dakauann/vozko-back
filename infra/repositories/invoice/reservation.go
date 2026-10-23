package invoice_repository

import (
	"errors"
	"fmt"
	"time"

	"vozko/domain/invoice"
	"vozko/domain/payment"
	"vozko/infra/database/schema"

	"gorm.io/gorm/clause"
)

var errChargeAlreadyAttached = errors.New("invoice already carries another charge")

func (r *repository) Reserve(inv *invoice.Invoice) (*invoice.Invoice, bool, error) {
	model := toSchema(inv)
	result := r.db.Clauses(clause.OnConflict{DoNothing: true}).Create(model)
	if result.Error != nil {
		return nil, false, result.Error
	}
	if result.RowsAffected == 1 {
		return toDomain(model), true, nil
	}
	if inv.IdempotencyKey == "" {
		return nil, false, fmt.Errorf("reserve invoice %s: insert conflicted without an idempotency key", inv.ID)
	}
	existing, err := r.getByIdempotencyKey(inv.IdempotencyKey)
	if err != nil {
		return nil, false, err
	}
	if existing == nil {
		return nil, false, fmt.Errorf("reserve invoice %s: key conflicted but no invoice holds it", inv.ID)
	}
	return existing, false, nil
}

func (r *repository) AttachCharge(id string, charge invoice.ChargeAttachment) error {
	result := r.db.Model(&schema.Invoice{}).
		Where("id = ? AND (external_id IS NULL OR external_id = ?)", id, charge.ExternalID).
		Updates(map[string]interface{}{
			"external_id":       charge.ExternalID,
			"pix_qr_code":       charge.PixQrCode,
			"pix_copy":          charge.PixCopy,
			"bank_slip_url":     charge.BankSlipUrl,
			"invoice_url":       charge.InvoiceUrl,
			"charge_expires_at": charge.ExpiresAt,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("attach charge %s to invoice %s: %w", charge.ExternalID, id, errChargeAlreadyAttached)
	}
	return nil
}

func (r *repository) CancelStaleReservations(createdBefore time.Time) (int64, error) {
	result := r.db.Model(&schema.Invoice{}).
		Where("external_id IS NULL AND status = ? AND created_at < ?", string(invoice.StatusPending), createdBefore).
		Update("status", string(invoice.StatusCancelled))
	return result.RowsAffected, result.Error
}

const openObligationsSQL = `SELECT provider,
	COUNT(*) FILTER (WHERE status IN (?) AND created_at > ?) AS unsettled,
	COUNT(*) FILTER (WHERE status IN (?) AND paid_at IS NOT NULL AND (
		(billing_type = ? AND paid_at > ?) OR
		(billing_type NOT IN (?) AND paid_at > ?))) AS reversible
FROM invoices
WHERE provider <> ''
GROUP BY provider`

func (r *repository) OpenObligations(now time.Time) ([]payment.Obligations, error) {
	var rows []struct {
		Provider   string
		Unsettled  int
		Reversible int
	}
	err := r.db.Raw(openObligationsSQL,
		[]string{string(invoice.StatusPending), string(invoice.StatusOverdue)},
		now.Add(-payment.UnsettledObligationWindow),
		[]string{string(invoice.StatusPaid), string(invoice.StatusReversed)},
		string(payment.MethodPix), now.Add(-payment.ReversalWindow(string(payment.MethodPix))).Unix(),
		[]string{string(payment.MethodPix), string(payment.MethodBoleto)}, now.Add(-payment.ReversalWindow(string(payment.MethodCreditCard))).Unix(),
	).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]payment.Obligations, 0, len(rows))
	for _, row := range rows {
		out = append(out, payment.Obligations{Provider: payment.Provider(row.Provider), Unsettled: row.Unsettled, Reversible: row.Reversible})
	}
	return out, nil
}

func nullableText(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func textOf(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
