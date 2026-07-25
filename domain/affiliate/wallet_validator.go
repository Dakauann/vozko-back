package affiliate

import "context"

type WalletValidator interface {
	ValidateWallet(ctx context.Context, input WalletValidationInput) error
}

type WalletValidationInput struct {
	WalletID     string
	CustomerName string
	CustomerDoc  string
}
