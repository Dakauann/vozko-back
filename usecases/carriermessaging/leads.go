package carriermessaging_usecase

import (
	"context"
	"errors"

	"vozko/domain/carriermessaging"
	"vozko/domain/lead"
)

type LeadStore interface {
	FindByNumber(workspaceID, number string) (*lead.Lead, error)
	FindOrCreate(workspaceID, number string, update lead.LeadUpdate) (*lead.Lead, bool, error)
}

type leadResolver struct {
	store LeadStore
}

func NewLeadResolver(store LeadStore) LeadResolver {
	return &leadResolver{store: store}
}

func (r *leadResolver) EnsureLead(_ context.Context, workspaceID, phone, name string) (*lead.Lead, error) {
	number := lead.NormalizeNumber(phone)
	if number == "" {
		return nil, carriermessaging.ErrPhoneInvalid
	}
	existing, err := r.store.FindByNumber(workspaceID, number)
	switch {
	case err == nil && existing != nil:
		return existing, nil
	case err != nil && !errors.Is(err, lead.ErrLeadNotFound):
		return nil, err
	}
	created, _, err := r.store.FindOrCreate(workspaceID, number, lead.LeadUpdate{Source: lead.SourceChannel, Name: name})
	if err != nil {
		return nil, err
	}
	return created, nil
}
