package sip_trunk

import (
	"context"
	"errors"
)

var ErrNoDialableTrunk = errors.New("no trunk in this workspace can place calls right now")

type CallPlanInput struct {
	WorkspaceID string
	UserID      string
	IsAdmin     bool
	TrunkID     string
	PhoneNumber string
}

type TrunkChoice struct {
	ID   string
	Name string
}

type CallPlan struct {
	PhoneNumber string
	Trunks      []TrunkChoice
}

func (p CallPlan) Chosen() (TrunkChoice, bool) {
	if len(p.Trunks) != 1 {
		return TrunkChoice{}, false
	}
	return p.Trunks[0], true
}

type CallPlanner interface {
	Plan(ctx context.Context, input CallPlanInput) (*CallPlan, error)
}

type CallLines interface {
	Lines(ctx context.Context, input CallPlanInput) ([]TrunkChoice, error)
}
