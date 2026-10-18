package smartcampaign_usecase

import (
	"context"
	"fmt"

	"vozko/domain/campaign"
	wc "vozko/domain/whatsapp_campaign"
	sc "vozko/domain/whatsapp_campaign/smartcampaign"
	"vozko/domain/working_hours"
	wd "vozko/domain/workspace/workspace_department"
	wsc "vozko/domain/workspace_config"
)

type WorkspaceConfigs interface {
	GetByWorkspaceID(ctx context.Context, workspaceID string) (*wsc.WorkspaceConfig, error)
}

type Departments interface {
	GetDepartmentByID(id string) (*wd.Department, error)
}

type Defaults struct {
	SendHours *working_hours.Spec     `json:"sendHours"`
	Presets   map[sc.Preset]sc.Policy `json:"presets"`
}

type Configurator struct {
	policies    sc.PolicyRepository
	workspaces  WorkspaceConfigs
	departments Departments
}

func NewConfigurator(policies sc.PolicyRepository, workspaces WorkspaceConfigs, departments Departments) *Configurator {
	return &Configurator{policies: policies, workspaces: workspaces, departments: departments}
}

func (c *Configurator) Save(target *wc.Campaign, policy sc.Policy) error {
	if target.Status == campaign.StatusRunning {
		return sc.ErrPolicyLocked
	}
	if err := policy.Validate(); err != nil {
		return err
	}
	return c.policies.SavePolicy(target.ID, policy)
}

func (c *Configurator) Remove(target *wc.Campaign) error {
	if target.Status == campaign.StatusRunning {
		return sc.ErrPolicyLocked
	}
	return c.policies.DeletePolicy(target.ID)
}

func (c *Configurator) Defaults(ctx context.Context, target *wc.Campaign) (Defaults, error) {
	hours, err := c.defaultHours(ctx, target)
	if err != nil {
		return Defaults{}, err
	}
	presets := make(map[sc.Preset]sc.Policy, len(sc.Presets))
	for _, preset := range sc.Presets {
		presets[preset] = sc.PresetPolicy(preset, hours)
	}
	return Defaults{SendHours: hours, Presets: presets}, nil
}

func (c *Configurator) defaultHours(ctx context.Context, target *wc.Campaign) (*working_hours.Spec, error) {
	config, err := c.workspaces.GetByWorkspaceID(ctx, target.WorkspaceID)
	if err != nil {
		return nil, fmt.Errorf("workspace hours of %s: %w", target.WorkspaceID, err)
	}
	var workspace, department *working_hours.Spec
	if config != nil {
		workspace = config.WorkingHours
	}
	if target.DepartmentID != "" {
		dept, err := c.departments.GetDepartmentByID(target.DepartmentID)
		if err != nil {
			return nil, fmt.Errorf("department hours of %s: %w", target.DepartmentID, err)
		}
		if dept != nil {
			department = dept.WorkingHours
		}
	}
	return working_hours.Resolve(workspace, department), nil
}
