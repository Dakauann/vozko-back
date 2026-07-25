package billing

// ConfirmMonthlyBillingUseCase advances a workspace's plan and active addon subscriptions to their
// next billing period when its unified MONTHLY_BILLING invoice is confirmed paid. It is the
// subscription-side effect of payment confirmation; the saldo credit is handled by the payment
// webhook itself.
type ConfirmMonthlyBillingUseCase interface {
	Execute(workspaceID string) error
}

// EmitMonthlyInvoicesUseCase composes and issues one unified MONTHLY_BILLING invoice per due
// workspace (plan + active channel addons). Execute returns the number of invoices emitted. It is
// idempotent per workspace/anchor cycle, so the cron may run it repeatedly within the emit window.
type EmitMonthlyInvoicesUseCase interface {
	Execute() (int, error)
}

// CancelSweepUseCase registers a 360dialog cancellation_request for every channel whose unified
// monthly invoice went unpaid through dunning, so the vendor never bills the next month for an
// uncollected channel. Execute returns the number of workspaces swept and self-gates to the cutoff.
type CancelSweepUseCase interface {
	Execute() (int, error)
}
