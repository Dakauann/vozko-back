package workspace_addon_usecase

import (
	"errors"
	"strings"

	"github.com/google/uuid"

	workspace_addon "vozko/domain/workspace/workspace_addon"
)

type createAddonDefinitionUseCase struct {
	repo workspace_addon.AddonDefinitionRepository
}

func NewCreateAddonDefinitionUseCase(repo workspace_addon.AddonDefinitionRepository) workspace_addon.CreateAddonDefinitionUseCase {
	return &createAddonDefinitionUseCase{repo: repo}
}

func (uc *createAddonDefinitionUseCase) Execute(input workspace_addon.AddonDefinitionMutationInput) (*workspace_addon.AddonDefinition, error) {
	key := strings.TrimSpace(input.Key)
	if existing, err := uc.repo.GetByKey(key); err == nil && existing != nil {
		return nil, workspace_addon.ErrAddonKeyExists
	} else if err != nil && !errors.Is(err, workspace_addon.ErrAddonNotFound) {
		return nil, err
	}

	units := input.UnitsPerQuantity
	if units <= 0 {
		units = 1
	}
	def := &workspace_addon.AddonDefinition{
		ID:                 uuid.NewString(),
		Key:                key,
		Name:               strings.TrimSpace(input.Name),
		Description:        input.Description,
		EntitlementKind:    input.EntitlementKind,
		UnitsPerQuantity:   units,
		MonthlyPriceMicros: input.MonthlyPriceMicros,
		AnnualPriceMicros:  input.AnnualPriceMicros,
		MonthlyCostMicros:  input.MonthlyCostMicros,
		AnnualCostMicros:   input.AnnualCostMicros,
		IsActive:           true,
		IsGloballyVisible:  true,
	}
	if input.IsActive != nil {
		def.IsActive = *input.IsActive
	}
	if input.IsGloballyVisible != nil {
		def.IsGloballyVisible = *input.IsGloballyVisible
	}
	if err := def.Validate(); err != nil {
		return nil, err
	}
	if err := uc.repo.Create(def); err != nil {
		return nil, err
	}
	return def, nil
}

type updateAddonDefinitionUseCase struct {
	repo workspace_addon.AddonDefinitionRepository
}

func NewUpdateAddonDefinitionUseCase(repo workspace_addon.AddonDefinitionRepository) workspace_addon.UpdateAddonDefinitionUseCase {
	return &updateAddonDefinitionUseCase{repo: repo}
}

func (uc *updateAddonDefinitionUseCase) Execute(id string, input workspace_addon.AddonDefinitionMutationInput) (*workspace_addon.AddonDefinition, error) {
	def, err := uc.repo.GetByID(id)
	if err != nil {
		return nil, err
	}
	if def.IsArchived() {
		return nil, workspace_addon.ErrAddonArchived
	}
	def.Name = strings.TrimSpace(input.Name)
	def.Description = input.Description
	def.EntitlementKind = input.EntitlementKind
	if input.UnitsPerQuantity > 0 {
		def.UnitsPerQuantity = input.UnitsPerQuantity
	}
	def.MonthlyPriceMicros = input.MonthlyPriceMicros
	def.AnnualPriceMicros = input.AnnualPriceMicros
	def.MonthlyCostMicros = input.MonthlyCostMicros
	def.AnnualCostMicros = input.AnnualCostMicros
	if input.IsActive != nil {
		def.IsActive = *input.IsActive
	}
	if input.IsGloballyVisible != nil {
		def.IsGloballyVisible = *input.IsGloballyVisible
	}
	if err := def.Validate(); err != nil {
		return nil, err
	}
	if err := uc.repo.Update(def); err != nil {
		return nil, err
	}
	return def, nil
}

type archiveAddonDefinitionUseCase struct {
	repo workspace_addon.AddonDefinitionRepository
	now  clockFn
}

func NewArchiveAddonDefinitionUseCase(repo workspace_addon.AddonDefinitionRepository) workspace_addon.ArchiveAddonDefinitionUseCase {
	return &archiveAddonDefinitionUseCase{repo: repo, now: utcNow}
}

func (uc *archiveAddonDefinitionUseCase) Execute(id string) error {
	return uc.repo.Archive(id, uc.now())
}

type listAddonDefinitionsUseCase struct {
	repo workspace_addon.AddonDefinitionRepository
}

func NewListAddonDefinitionsUseCase(repo workspace_addon.AddonDefinitionRepository) workspace_addon.ListAddonDefinitionsUseCase {
	return &listAddonDefinitionsUseCase{repo: repo}
}

func (uc *listAddonDefinitionsUseCase) Execute(includeArchived bool) ([]*workspace_addon.AddonDefinition, error) {
	return uc.repo.List(includeArchived)
}

type getAddonDefinitionUseCase struct {
	repo workspace_addon.AddonDefinitionRepository
}

func NewGetAddonDefinitionUseCase(repo workspace_addon.AddonDefinitionRepository) workspace_addon.GetAddonDefinitionUseCase {
	return &getAddonDefinitionUseCase{repo: repo}
}

func (uc *getAddonDefinitionUseCase) Execute(id string) (*workspace_addon.AddonDefinition, error) {
	return uc.repo.GetByID(id)
}
