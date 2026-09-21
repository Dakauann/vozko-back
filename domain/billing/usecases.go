package billing

type ConfirmMonthlyBillingUseCase interface {
	Execute(workspaceID string) error
}

type EmitMonthlyInvoicesUseCase interface {
	Execute() (int, error)
}

type CancelSweepUseCase interface {
	Execute() (int, error)
}
