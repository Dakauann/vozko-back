package billing

import "context"

type OpsAlerter interface {
	Alert(ctx context.Context, subject, detail string) error
}
