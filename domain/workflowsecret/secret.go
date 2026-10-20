package workflowsecret

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"vozko/domain/shared"
)

const (
	MaxNameLength = shared.MaxIdentifierNameLength
	MaxValueBytes = 8 * 1024
)

var (
	ErrInvalidName       = errors.New("workflowsecret: the name uses only lowercase letters, digits and underscore, 1 to 64 characters")
	ErrValueRequired     = errors.New("workflowsecret: the value is empty")
	ErrValueTooLong      = errors.New("workflowsecret: the value is longer than 8 KB")
	ErrWorkspaceRequired = errors.New("workflowsecret: a secret needs its workspace")
	ErrNotFound          = errors.New("workflowsecret: secret not found")
	ErrMissing           = errors.New("workflowsecret: referenced secret does not exist")
	ErrUnavailable       = errors.New("workflowsecret: workspace secrets are not available here")
)

type Secret struct {
	ID             string
	WorkspaceID    string
	Name           string
	EncryptedValue []byte
	CreatedBy      string
	UpdatedBy      string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type Summary struct {
	Name      string
	UpdatedAt time.Time
	UpdatedBy string
}

func (s Secret) Summary() Summary {
	return Summary{Name: s.Name, UpdatedAt: s.UpdatedAt, UpdatedBy: s.UpdatedBy}
}

func ValidateName(name string) error {
	if !shared.ValidIdentifierName(name) {
		return ErrInvalidName
	}
	return nil
}

func ValidateValue(value string) error {
	if value == "" {
		return ErrValueRequired
	}
	if len(value) > MaxValueBytes {
		return ErrValueTooLong
	}
	return nil
}

func UniqueNames(names []string) []string {
	seen := make(map[string]bool, len(names))
	out := make([]string, 0, len(names))
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	return out
}

func MissingNames(names []string, resolved map[string]string) []string {
	var missing []string
	for _, n := range UniqueNames(names) {
		if _, ok := resolved[n]; !ok {
			missing = append(missing, n)
		}
	}
	sort.Strings(missing)
	return missing
}

type MissingError struct {
	Names []string
}

func (e *MissingError) Error() string {
	return fmt.Sprintf("segredo não encontrado neste workspace: %s (crie em Segredos do workspace)", strings.Join(e.Names, ", "))
}

func (e *MissingError) Is(target error) bool { return target == ErrMissing }
