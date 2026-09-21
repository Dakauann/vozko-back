package unofficial_whatsapp

import (
	"context"
	"errors"
	"fmt"
)

var (
	ErrNoInstanceAllowance  = errors.New("this workspace has no unofficial whatsapp numbers included")
	ErrInstanceLimitReached = errors.New("all unofficial whatsapp numbers included in this workspace are in use")
)

type InstanceAllowance struct {
	Limit int
	Used  int

	Granted   int
	Purchased int
}

func (a InstanceAllowance) Remaining() int {
	if a.Used >= a.Limit {
		return 0
	}
	return a.Limit - a.Used
}

func (a InstanceAllowance) CanProvision() bool { return a.Used < a.Limit }

func (a InstanceAllowance) OverLimit() bool { return a.Used > a.Limit }

func (a InstanceAllowance) Enforce() error {
	if a.Limit <= 0 {
		return ErrNoInstanceAllowance
	}
	if !a.CanProvision() {
		return fmt.Errorf("%w: %d of %d in use", ErrInstanceLimitReached, a.Used, a.Limit)
	}
	return nil
}

type InstanceEntitlementReader interface {
	AllowanceFor(ctx context.Context, workspaceID string) (InstanceAllowance, error)
}

var ErrEntitlementUnavailable = errors.New("unofficial whatsapp: could not resolve this workspace's number allowance")
