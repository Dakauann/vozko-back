package emailsending

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

type Mode string

const (
	ModeProbation Mode = "probation"
	ModeTrusted   Mode = "trusted"
	ModeOwnDomain Mode = "own_domain"
)

func (m Mode) Valid() bool {
	switch m {
	case ModeProbation, ModeTrusted, ModeOwnDomain:
		return true
	}
	return false
}

type AccountStatus string

const (
	AccountActive           AccountStatus = "active"
	AccountPausedByProvider AccountStatus = "paused_by_provider"
	AccountPausedByPlatform AccountStatus = "paused_by_platform"
	AccountDormant          AccountStatus = "dormant"
)

func (s AccountStatus) Valid() bool {
	switch s {
	case AccountActive, AccountPausedByProvider, AccountPausedByPlatform, AccountDormant:
		return true
	}
	return false
}

func SendableStatuses() []AccountStatus {
	return []AccountStatus{AccountActive, AccountDormant}
}

const maxTenantNameLength = 64

type SenderAccount struct {
	ID                 string
	WorkspaceID        string
	Provider           string
	TenantName         string
	TenantRef          string
	Mode               Mode
	Status             AccountStatus
	StatusReason       string
	Slug               string
	ProbationStartedAt time.Time
	PromotedAt         *time.Time
	UnusedWarnedAt     *time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

func TenantNameFor(workspaceID string) (string, error) {
	id := strings.TrimSpace(workspaceID)
	if id == "" {
		return "", fmt.Errorf("%w: empty workspace", ErrInvalidTenantName)
	}
	name := "ws-" + id
	if len(name) > maxTenantNameLength {
		return "", fmt.Errorf("%w: too long", ErrInvalidTenantName)
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9') && r != '-' && r != '_' {
			return "", fmt.Errorf("%w: %q", ErrInvalidTenantName, r)
		}
	}
	return name, nil
}

func (a SenderAccount) CanSend() error {
	if strings.TrimSpace(a.TenantName) == "" || !a.Mode.Valid() {
		return ErrAccountNotProvisioned
	}
	switch a.Status {
	case AccountActive:
		return nil
	case AccountDormant:
		return ErrAccountDormant
	}
	return ErrAccountPaused
}

func (a SenderAccount) CanSendOnDemand() error {
	err := a.CanSend()
	if errors.Is(err, ErrAccountDormant) {
		return nil
	}
	return err
}
