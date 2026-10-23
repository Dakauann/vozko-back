package emailinbox

import (
	"context"
	"net/netip"

	balance_usecase "vozko/usecases/balance"
)

func (f *fakeRaw) Delete(_ context.Context, key string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.deleted = append(f.deleted, key)
	return nil
}

type fakeSenderIP struct {
	ip  netip.Addr
	ok  bool
	got [][]byte
}

func (f *fakeSenderIP) ConnectingIP(raw []byte) (netip.Addr, bool) {
	f.got = append(f.got, raw)
	return f.ip, f.ok
}

type fakeGuard struct {
	senders  []netip.Addr
	paused   []string
	pauseErr error
}

func (f *fakeGuard) RecordSender(_ context.Context, ip netip.Addr) {
	f.senders = append(f.senders, ip)
}

func (f *fakeGuard) Pause(_ context.Context, workspaceID string) error {
	f.paused = append(f.paused, workspaceID)
	return f.pauseErr
}

type receiveCall struct {
	workspaceID string
	key         string
	size        int64
}

type fakeReceiveBilling struct {
	calls        []receiveCall
	balanceAfter int64
	err          error
	order        *[]string
}

func (f *fakeReceiveBilling) ChargeReceive(_ context.Context, workspaceID, key string, size int64) (balance_usecase.EmailReceiveCharge, error) {
	if f.order != nil {
		*f.order = append(*f.order, "charge_receive")
	}
	if f.err != nil {
		return balance_usecase.EmailReceiveCharge{}, f.err
	}
	f.calls = append(f.calls, receiveCall{workspaceID: workspaceID, key: key, size: size})
	return balance_usecase.EmailReceiveCharge{TotalMicros: 583, BalanceAfterMicros: f.balanceAfter, Charged: true}, nil
}
