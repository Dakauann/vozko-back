package emailapi_usecase

import (
	"context"
	"strings"
	"testing"

	"vozko/domain/balance"
	"vozko/domain/emailapi"
	"vozko/domain/notification"
	tx "vozko/usecases/transactionalemail"
)

type walletFake struct{ amount int64 }

func (w walletFake) GetByWorkspaceID(string) (*balance.Balance, error) {
	return &balance.Balance{Amount: w.amount}, nil
}

type episodesFake struct {
	published []notification.Draft
	resolved  []string
}

func (e *episodesFake) Publish(_ context.Context, d notification.Draft) (notification.Record, error) {
	e.published = append(e.published, d)
	return notification.Record{}, nil
}

func (e *episodesFake) Resolve(_ context.Context, _ string, keys ...string) error {
	e.resolved = append(e.resolved, keys...)
	return nil
}

func TestTheSystemReserveAlertsBelowTwoThousandEmailsAndResolvesAbove(t *testing.T) {
	billing := newBilling(0)
	alerts := &episodesFake{}
	low := ReserveWatch{WorkspaceID: "sys", Balances: walletFake{amount: 583 * 150}, Quotes: billing, Alerts: alerts}
	if err := low.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(alerts.published) != 1 {
		t.Fatalf("published = %d", len(alerts.published))
	}
	d := alerts.published[0]
	if d.EpisodeKey != ReserveKey("sys") || d.Severity != notification.SeverityCritical || !d.Email || !strings.Contains(d.Body, "150 e-mails") || strings.ContainsAny(d.Title+d.Body, "–—") {
		t.Fatalf("draft = %+v", d)
	}
	ok := ReserveWatch{WorkspaceID: "sys", Balances: walletFake{amount: 583 * emailapi.SystemEmailReserve}, Quotes: billing, Alerts: alerts}
	if err := ok.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(alerts.resolved) != 1 || alerts.resolved[0] != ReserveKey("sys") || len(alerts.published) != 1 {
		t.Fatalf("resolved = %v", alerts.resolved)
	}
}

func TestAnUnboundRelayRefusesInsteadOfDroppingTheEmail(t *testing.T) {
	r := NewRelay()
	if _, err := r.Send(context.Background(), emailapi.Request{}); tx.CodeOf(err) != tx.CodeUnavailable {
		t.Fatalf("unbound = %v", err)
	}
	h := newHarness()
	r.Bind(h.service(t))
	if _, err := r.Send(context.Background(), request("ana@example.com")); err != nil {
		t.Fatal(err)
	}
}
