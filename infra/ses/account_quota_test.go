package ses

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	"github.com/aws/aws-sdk-go-v2/service/sesv2/types"
)

type fakeAccountAPI struct {
	out *sesv2.GetAccountOutput
	err error
}

func (f fakeAccountAPI) GetAccount(context.Context, *sesv2.GetAccountInput, ...func(*sesv2.Options)) (*sesv2.GetAccountOutput, error) {
	return f.out, f.err
}

func TestReadSendQuotaReturnsTheAccountQuota(t *testing.T) {
	quota := NewAccountQuota(fakeAccountAPI{out: &sesv2.GetAccountOutput{
		ProductionAccessEnabled: true,
		SendQuota:               &types.SendQuota{MaxSendRate: 14, Max24HourSend: 50000, SentLast24Hours: 120},
	}})
	got, err := quota.ReadSendQuota(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.MaxSendRate != 14 || got.Max24HourSend != 50000 || got.SentLast24Hours != 120 || !got.ProductionAccess || !got.Read {
		t.Fatalf("quota = %+v", got)
	}
}

func TestReadSendQuotaRefusesAnAnswerWithoutAQuota(t *testing.T) {
	cases := []struct {
		name string
		api  fakeAccountAPI
	}{
		{name: "provider error", api: fakeAccountAPI{err: errors.New("throttled")}},
		{name: "empty answer", api: fakeAccountAPI{out: &sesv2.GetAccountOutput{}}},
		{name: "no answer", api: fakeAccountAPI{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewAccountQuota(tc.api).ReadSendQuota(context.Background()); err == nil {
				t.Fatal("a quota that could not be read must be an error, never a default")
			}
		})
	}
}
