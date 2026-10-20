package workflowsecret_usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"vozko/domain/workflowsecret"
)

var (
	errSecretsRepositoryRequired = errors.New("workflowsecret: the secrets repository is required")
	errCipherRequired            = errors.New("workflowsecret: the cipher is required")
)

type Deps struct {
	Secrets workflowsecret.Repository
	Cipher  workflowsecret.Cipher
	Now     func() time.Time
	NewID   func() string
}

type Service struct {
	deps Deps
}

var _ workflowsecret.UseCases = (*Service)(nil)

func New(deps Deps) (*Service, error) {
	if deps.Secrets == nil {
		return nil, errSecretsRepositoryRequired
	}
	if deps.Cipher == nil {
		return nil, errCipherRequired
	}
	if deps.Now == nil {
		deps.Now = time.Now
	}
	if deps.NewID == nil {
		deps.NewID = uuid.NewString
	}
	return &Service{deps: deps}, nil
}

func (s *Service) Put(ctx context.Context, workspaceID, userID, name, value string) (workflowsecret.Summary, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return workflowsecret.Summary{}, workflowsecret.ErrWorkspaceRequired
	}
	if err := workflowsecret.ValidateName(name); err != nil {
		return workflowsecret.Summary{}, err
	}
	if err := workflowsecret.ValidateValue(value); err != nil {
		return workflowsecret.Summary{}, err
	}
	sealed, err := s.deps.Cipher.Encrypt([]byte(value))
	if err != nil {
		return workflowsecret.Summary{}, fmt.Errorf("workflowsecret: encrypt: %w", err)
	}
	now := s.deps.Now().UTC()
	stored, err := s.deps.Secrets.Upsert(ctx, workflowsecret.Secret{
		ID:             s.deps.NewID(),
		WorkspaceID:    workspaceID,
		Name:           name,
		EncryptedValue: sealed,
		CreatedBy:      userID,
		UpdatedBy:      userID,
		CreatedAt:      now,
		UpdatedAt:      now,
	})
	if err != nil {
		return workflowsecret.Summary{}, err
	}
	return stored.Summary(), nil
}

func (s *Service) Delete(ctx context.Context, workspaceID, name string) error {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return workflowsecret.ErrWorkspaceRequired
	}
	if err := workflowsecret.ValidateName(name); err != nil {
		return err
	}
	return s.deps.Secrets.Delete(ctx, workspaceID, name)
}

func (s *Service) List(ctx context.Context, workspaceID string) ([]workflowsecret.Summary, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, workflowsecret.ErrWorkspaceRequired
	}
	return s.deps.Secrets.List(ctx, workspaceID)
}

func (s *Service) Resolve(ctx context.Context, workspaceID string, names []string) (map[string]string, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, workflowsecret.ErrWorkspaceRequired
	}
	wanted := make([]string, 0, len(names))
	for _, name := range workflowsecret.UniqueNames(names) {
		if workflowsecret.ValidateName(name) == nil {
			wanted = append(wanted, name)
		}
	}
	resolved := make(map[string]string, len(wanted))
	if len(wanted) == 0 {
		return resolved, nil
	}
	rows, err := s.deps.Secrets.FindByNames(ctx, workspaceID, wanted)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		plain, err := s.deps.Cipher.Decrypt(row.EncryptedValue)
		if err != nil {
			return nil, fmt.Errorf("workflowsecret: decrypt %q: %w", row.Name, err)
		}
		resolved[row.Name] = string(plain)
	}
	return resolved, nil
}
