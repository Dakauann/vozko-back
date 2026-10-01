package sip_trunk_usecase

import (
	"context"
	"strings"

	"vozko/domain/sip_trunk"
)

type CreateTrunkInput struct {
	WorkspaceID string
	Name        string
	TrunkType   sip_trunk.TrunkType
	Host        string
	Port        int
	Domain      string
	Transport   sip_trunk.Transport
	Username    string
	Password    string
	Enabled     bool
	Settings    sip_trunk.Settings
}

type UpdateTrunkInput struct {
	WorkspaceID string
	ID          string
	Name        *string
	TrunkType   *sip_trunk.TrunkType
	Host        *string
	Port        *int
	Domain      *string
	Transport   *sip_trunk.Transport
	Username    *string
	Password    *string
	Enabled     *bool
	Settings    *sip_trunk.Settings
}

type CreateTrunkUseCase struct {
	repo   sip_trunk.Repository
	engine sip_trunk.Engine
}

func NewCreateTrunkUseCase(repo sip_trunk.Repository, engine sip_trunk.Engine) *CreateTrunkUseCase {
	return &CreateTrunkUseCase{repo: repo, engine: engine}
}

func (uc *CreateTrunkUseCase) Execute(ctx context.Context, input CreateTrunkInput) (*sip_trunk.SIPTrunk, error) {
	trunk := DraftTrunk(input)
	if err := trunk.Validate(); err != nil {
		return nil, err
	}
	if err := uc.repo.Create(ctx, trunk); err != nil {
		return nil, err
	}
	if trunk.Enabled {
		return withEngineOutcome(uc.engine, trunk, uc.engine.RegisterTrunk(trunk)), nil
	}
	return trunk, nil
}

func DraftTrunk(input CreateTrunkInput) *sip_trunk.SIPTrunk {
	trunk := &sip_trunk.SIPTrunk{
		WorkspaceID: input.WorkspaceID,
		Name:        strings.TrimSpace(input.Name),
		TrunkType:   input.TrunkType,
		Host:        strings.TrimSpace(input.Host),
		Port:        input.Port,
		Domain:      strings.TrimSpace(input.Domain),
		Transport:   input.Transport,
		Username:    strings.TrimSpace(input.Username),
		Password:    input.Password,
		Enabled:     input.Enabled,
		Settings:    input.Settings,
	}
	if trunk.TrunkType == "" {
		trunk.TrunkType = sip_trunk.TrunkTypeBidirectional
	}
	if trunk.Transport == "" {
		trunk.Transport = sip_trunk.TransportUDP
	}
	return trunk
}

type UpdateTrunkUseCase struct {
	repo   sip_trunk.Repository
	engine sip_trunk.Engine
}

func NewUpdateTrunkUseCase(repo sip_trunk.Repository, engine sip_trunk.Engine) *UpdateTrunkUseCase {
	return &UpdateTrunkUseCase{repo: repo, engine: engine}
}

func (uc *UpdateTrunkUseCase) Execute(ctx context.Context, input UpdateTrunkInput) (*sip_trunk.SIPTrunk, error) {
	trunk, err := uc.repo.FindInWorkspace(ctx, input.WorkspaceID, input.ID)
	if err != nil {
		return nil, err
	}
	ApplyUpdate(trunk, input)
	if err := trunk.Validate(); err != nil {
		return nil, err
	}
	if err := uc.repo.Update(ctx, trunk); err != nil {
		return nil, err
	}
	return withEngineOutcome(uc.engine, trunk, uc.engine.RefreshTrunk(trunk)), nil
}

func ApplyUpdate(trunk *sip_trunk.SIPTrunk, input UpdateTrunkInput) {
	if input.Name != nil {
		trunk.Name = strings.TrimSpace(*input.Name)
	}
	if input.TrunkType != nil {
		trunk.TrunkType = *input.TrunkType
	}
	if input.Host != nil {
		trunk.Host = strings.TrimSpace(*input.Host)
	}
	if input.Port != nil {
		trunk.Port = *input.Port
	}
	if input.Domain != nil {
		trunk.Domain = strings.TrimSpace(*input.Domain)
	}
	if input.Transport != nil {
		trunk.Transport = *input.Transport
	}
	if input.Username != nil {
		trunk.Username = strings.TrimSpace(*input.Username)
	}
	if input.Password != nil && *input.Password != "" {
		trunk.Password = *input.Password
	}
	if input.Enabled != nil {
		trunk.Enabled = *input.Enabled
	}
	if input.Settings != nil {
		trunk.Settings = *input.Settings
	}
}

type DeleteTrunkUseCase struct {
	repo   sip_trunk.Repository
	engine sip_trunk.Engine
}

func NewDeleteTrunkUseCase(repo sip_trunk.Repository, engine sip_trunk.Engine) *DeleteTrunkUseCase {
	return &DeleteTrunkUseCase{repo: repo, engine: engine}
}

func (uc *DeleteTrunkUseCase) Execute(ctx context.Context, workspaceID, id string) error {
	trunk, err := uc.repo.FindInWorkspace(ctx, workspaceID, id)
	if err != nil {
		return err
	}
	if err := uc.engine.UnregisterTrunk(trunk.ID); err != nil {
		return err
	}
	return uc.repo.Delete(ctx, workspaceID, trunk.ID)
}

type ListTrunksUseCase struct {
	repo   sip_trunk.Repository
	engine sip_trunk.Engine
}

func NewListTrunksUseCase(repo sip_trunk.Repository, engine sip_trunk.Engine) *ListTrunksUseCase {
	return &ListTrunksUseCase{repo: repo, engine: engine}
}

func (uc *ListTrunksUseCase) Execute(ctx context.Context, workspaceID string) ([]*sip_trunk.SIPTrunk, error) {
	trunks, err := uc.repo.ListByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	for _, trunk := range trunks {
		withLiveStatus(uc.engine, trunk)
	}
	return trunks, nil
}

type GetTrunkUseCase struct {
	repo   sip_trunk.Repository
	engine sip_trunk.Engine
}

func NewGetTrunkUseCase(repo sip_trunk.Repository, engine sip_trunk.Engine) *GetTrunkUseCase {
	return &GetTrunkUseCase{repo: repo, engine: engine}
}

func (uc *GetTrunkUseCase) Execute(ctx context.Context, workspaceID, id string) (*sip_trunk.SIPTrunk, error) {
	trunk, err := uc.repo.FindInWorkspace(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	return withLiveStatus(uc.engine, trunk), nil
}

func withLiveStatus(engine sip_trunk.Engine, trunk *sip_trunk.SIPTrunk) *sip_trunk.SIPTrunk {
	if status, ok := engine.TrunkStatus(trunk.ID); ok {
		trunk.RegistrationStatus = status.Status
		trunk.LastError = status.Error
	}
	return trunk
}

func withEngineOutcome(engine sip_trunk.Engine, trunk *sip_trunk.SIPTrunk, err error) *sip_trunk.SIPTrunk {
	if err != nil {
		trunk.RegistrationStatus = sip_trunk.RegistrationStatusFailed
		trunk.LastError = err.Error()
		return trunk
	}
	return withLiveStatus(engine, trunk)
}
