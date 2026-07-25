package asaas

import (
	"context"
	"errors"
	"fmt"

	"vozko/domain/affiliate"
)

type walletValidatorAdapter struct {
	svc AsaasServiceUseCases
}

func NewWalletValidator(svc AsaasServiceUseCases) affiliate.WalletValidator {
	return &walletValidatorAdapter{svc: svc}
}

func (a *walletValidatorAdapter) ValidateWallet(_ context.Context, input affiliate.WalletValidationInput) error {
	if a == nil || a.svc == nil {
		return fmt.Errorf("wallet validator: asaas service not configured")
	}
	err := a.svc.ValidateWalletID(input.CustomerName, input.CustomerDoc, input.WalletID)
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrInvalidWalletID) {
		return affiliate.ErrInvalidAsaasWalletID
	}

	return fmt.Errorf("%w: %v", affiliate.ErrWalletValidationFailed, err)
}
