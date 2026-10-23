package ses

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/sesv2"

	es "vozko/domain/emailsending"
)

var ErrNoSendQuota = errors.New("ses: the account answer carries no send quota")

type AccountAPI interface {
	GetAccount(ctx context.Context, in *sesv2.GetAccountInput, opts ...func(*sesv2.Options)) (*sesv2.GetAccountOutput, error)
}

type AccountQuota struct {
	api AccountAPI
}

var _ es.SendQuotaReader = AccountQuota{}

func NewAccountQuota(api AccountAPI) AccountQuota {
	return AccountQuota{api: api}
}

func (a AccountQuota) ReadSendQuota(ctx context.Context) (es.SendQuota, error) {
	out, err := a.api.GetAccount(ctx, &sesv2.GetAccountInput{})
	if err != nil {
		return es.SendQuota{}, fmt.Errorf("ses: get account: %w", err)
	}
	if out == nil || out.SendQuota == nil {
		return es.SendQuota{}, ErrNoSendQuota
	}
	return es.SendQuota{
		MaxSendRate:      out.SendQuota.MaxSendRate,
		Max24HourSend:    out.SendQuota.Max24HourSend,
		SentLast24Hours:  out.SendQuota.SentLast24Hours,
		ProductionAccess: out.ProductionAccessEnabled,
		Read:             true,
	}, nil
}
