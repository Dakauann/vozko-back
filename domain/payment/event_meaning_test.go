package payment

import "testing"

func TestEveryAsaasPaymentEventHasAMeaning(t *testing.T) {
	want := map[string]EventMeaning{
		"PAYMENT_CREATED":                         MeaningInformational,
		"PAYMENT_AWAITING_RISK_ANALYSIS":          MeaningInformational,
		"PAYMENT_APPROVED_BY_RISK_ANALYSIS":       MeaningInformational,
		"PAYMENT_REPROVED_BY_RISK_ANALYSIS":       MeaningCancelled,
		"PAYMENT_AUTHORIZED":                      MeaningInformational,
		"PAYMENT_UPDATED":                         MeaningInformational,
		"PAYMENT_CONFIRMED":                       MeaningInformational,
		"PAYMENT_RECEIVED":                        MeaningSettled,
		"PAYMENT_RECEIVED_IN_CASH":                MeaningSettled,
		"PAYMENT_CREDIT_CARD_CAPTURE_REFUSED":     MeaningCancelled,
		"PAYMENT_ANTICIPATED":                     MeaningInformational,
		"PAYMENT_OVERDUE":                         MeaningOverdue,
		"PAYMENT_DELETED":                         MeaningCancelled,
		"PAYMENT_RESTORED":                        MeaningInformational,
		"PAYMENT_REFUNDED":                        MeaningReversalFinal,
		"PAYMENT_PARTIALLY_REFUNDED":              MeaningReversalFinal,
		"PAYMENT_REFUND_IN_PROGRESS":              MeaningReversalOpened,
		"PAYMENT_REFUND_DENIED":                   MeaningInformational,
		"PAYMENT_RECEIVED_IN_CASH_UNDONE":         MeaningReversalFinal,
		"PAYMENT_CHARGEBACK_REQUESTED":            MeaningReversalOpened,
		"PAYMENT_CHARGEBACK_DISPUTE":              MeaningReversalInDispute,
		"PAYMENT_AWAITING_CHARGEBACK_REVERSAL":    MeaningReversalInDispute,
		"PAYMENT_DUNNING_REQUESTED":               MeaningInformational,
		"PAYMENT_DUNNING_RECEIVED":                MeaningInformational,
		"PAYMENT_BANK_SLIP_CANCELLED":             MeaningInformational,
		"PAYMENT_BANK_SLIP_VIEWED":                MeaningInformational,
		"PAYMENT_CHECKOUT_VIEWED":                 MeaningInformational,
		"PAYMENT_SPLIT_CANCELLED":                 MeaningInformational,
		"PAYMENT_SPLIT_DIVERGENCE_BLOCK":          MeaningInformational,
		"PAYMENT_SPLIT_DIVERGENCE_BLOCK_FINISHED": MeaningInformational,
		"PAYMENT_SPLIT_DONE":                      MeaningInformational,
	}
	for event, meaning := range want {
		got, ok := MeaningOf(event)
		if !ok {
			t.Errorf("%s has no meaning; it would be retried forever as unknown", event)
			continue
		}
		if got != meaning {
			t.Errorf("MeaningOf(%s) = %s, want %s", event, got, meaning)
		}
	}
}

func TestTheEventsMercadoPagoIsMappedToHaveAMeaning(t *testing.T) {
	want := map[string]EventMeaning{
		EventPaymentInAnalysis:         MeaningInformational,
		EventPaymentRejected:           MeaningCancelled,
		EventPaymentChargeback:         MeaningReversalFinal,
		EventPaymentInMediation:        MeaningReversalOpened,
		EventPaymentChargebackReversed: MeaningReversalReleased,
	}
	for event, meaning := range want {
		if got, ok := MeaningOf(event); !ok || got != meaning {
			t.Errorf("MeaningOf(%s) = %s, %v; want %s", event, got, ok, meaning)
		}
	}
}

func TestEventNamesAreReadCaseInsensitively(t *testing.T) {
	if got, ok := MeaningOf("  payment_chargeback_requested "); !ok || got != MeaningReversalOpened {
		t.Fatalf("MeaningOf(lowercase) = %s, %v", got, ok)
	}
}

func TestAnUnknownEventHasNoMeaning(t *testing.T) {
	for _, event := range []string{"", "PAYMENT_SOMETHING_NEW", "MERCADOPAGO_STATUS_FROZEN"} {
		if got, ok := MeaningOf(event); ok {
			t.Errorf("MeaningOf(%q) = %s; an unknown event must never be treated as known", event, got)
		}
	}
}

func TestReversalMeaningsAreTheOnesThatMoveMoneyBack(t *testing.T) {
	for _, m := range []EventMeaning{MeaningReversalOpened, MeaningReversalFinal} {
		if !m.TakesMoneyBack() {
			t.Errorf("%s must take the credit back", m)
		}
	}
	for _, m := range []EventMeaning{MeaningReversalInDispute, MeaningReversalReleased, MeaningSettled, MeaningInformational, MeaningOverdue, MeaningCancelled} {
		if m.TakesMoneyBack() {
			t.Errorf("%s must not take money back", m)
		}
	}
}

func TestReversalKindFollowsTheBillingType(t *testing.T) {
	cases := map[string]ReversalKind{
		"CREDIT_CARD": ReversalChargeback,
		"credit_card": ReversalChargeback,
		"PIX":         ReversalPixReturn,
		" pix ":       ReversalPixReturn,
		"BOLETO":      ReversalUnexpectedRefund,
		"":            ReversalUnexpectedRefund,
	}
	for billingType, want := range cases {
		if got := ReversalKindForBillingType(billingType); got != want {
			t.Errorf("ReversalKindForBillingType(%q) = %s, want %s", billingType, got, want)
		}
	}
}
