package branch_usecase

import (
	"strings"

	"vozko/domain/branch"
)

// --- Update -------------------------------------------------------------

type updateBranchUseCase struct {
	repo branch.Repository
}

func NewUpdateUseCase(repo branch.Repository) branch.UpdateUseCase {
	return &updateBranchUseCase{repo: repo}
}

func (uc *updateBranchUseCase) Execute(input branch.UpdateBranchInput) (*branch.Branch, error) {
	b, err := uc.repo.FindByID(strings.TrimSpace(input.ID))
	if err != nil {
		return nil, err
	}
	if err := b.CanBeModifiedBy(input.Actor); err != nil {
		return nil, err
	}

	if input.DisplayName != nil {
		b.DisplayName = *input.DisplayName
	}
	if input.Codecs != nil {
		b.Codecs = input.Codecs
	}
	if input.MaxContacts != nil {
		b.MaxContacts = *input.MaxContacts
	}
	if input.DND != nil {
		b.DND = *input.DND
	}
	b.Touch()

	if err := b.Validate(); err != nil {
		return nil, err
	}
	if err := uc.repo.Update(b); err != nil {
		return nil, err
	}
	return b, nil
}

// --- Enable / disable ----------------------------------------------------

type enableBranchUseCase struct {
	repo branch.Repository
}

func NewEnableUseCase(repo branch.Repository) branch.EnableUseCase {
	return &enableBranchUseCase{repo: repo}
}

func (uc *enableBranchUseCase) Execute(id string, enabled bool, actor branch.Actor) (*branch.Branch, error) {
	b, err := uc.repo.FindByID(strings.TrimSpace(id))
	if err != nil {
		return nil, err
	}
	if err := b.CanBeModifiedBy(actor); err != nil {
		return nil, err
	}
	b.Enabled = enabled
	b.Touch()
	if err := uc.repo.Update(b); err != nil {
		return nil, err
	}
	return b, nil
}

// --- Delete --------------------------------------------------------------

type deleteBranchUseCase struct {
	repo branch.Repository
}

func NewDeleteUseCase(repo branch.Repository) branch.DeleteUseCase {
	return &deleteBranchUseCase{repo: repo}
}

func (uc *deleteBranchUseCase) Execute(id string, actor branch.Actor) error {
	b, err := uc.repo.FindByID(strings.TrimSpace(id))
	if err != nil {
		return err
	}
	if err := b.CanBeModifiedBy(actor); err != nil {
		return err
	}
	return uc.repo.Delete(b.ID)
}

// --- Rotate secret -------------------------------------------------------

type rotateSecretUseCase struct {
	repo  branch.Repository
	realm string
}

func NewRotateSecretUseCase(repo branch.Repository, realm string) branch.RotateSecretUseCase {
	if strings.TrimSpace(realm) == "" {
		realm = defaultRealm
	}
	return &rotateSecretUseCase{repo: repo, realm: realm}
}

func (uc *rotateSecretUseCase) Execute(id string, actor branch.Actor) (*branch.SecretResult, error) {
	b, err := uc.repo.FindByID(strings.TrimSpace(id))
	if err != nil {
		return nil, err
	}
	if err := b.CanBeModifiedBy(actor); err != nil {
		return nil, err
	}

	secret, err := branch.GenerateSecret()
	if err != nil {
		return nil, err
	}
	b.SetSecret(uc.realm, secret)

	if err := uc.repo.Update(b); err != nil {
		return nil, err
	}
	return &branch.SecretResult{Branch: b, Secret: secret, Realm: b.Realm, SIPUser: b.SIPUser}, nil
}
