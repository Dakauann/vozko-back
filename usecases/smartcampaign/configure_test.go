package smartcampaign_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/campaign"
	sc "vozko/domain/whatsapp_campaign/smartcampaign"
	"vozko/domain/working_hours"
	wd "vozko/domain/workspace/workspace_department"
	wsc "vozko/domain/workspace_config"
)

type workspaceHours struct{ spec *working_hours.Spec }

func (w workspaceHours) GetByWorkspaceID(context.Context, string) (*wsc.WorkspaceConfig, error) {
	return &wsc.WorkspaceConfig{WorkingHours: w.spec}, nil
}

type departmentHours map[string]*working_hours.Spec

func (d departmentHours) GetDepartmentByID(id string) (*wd.Department, error) {
	return &wd.Department{ID: id, WorkingHours: d[id]}, nil
}

func saturdayMornings() *working_hours.Spec {
	return &working_hours.Spec{Timezone: "America/Sao_Paulo", Days: map[string][]working_hours.Window{"sat": {{Start: "09:00", End: "13:00"}}}}
}

func newConfigurator(s *store, workspace, department *working_hours.Spec) *Configurator {
	return NewConfigurator(s, workspaceHours{spec: workspace}, departmentHours{"sales": department})
}

func TestAPolicyCannotChangeWhileTheCampaignRuns(t *testing.T) {
	s := newStore()
	c := s.addCampaign("c1", 10, nil)
	c.Status = campaign.StatusRunning
	configurator := newConfigurator(s, sendHours(), nil)

	if err := configurator.Save(c, sc.PresetPolicy(sc.Standard, sendHours())); !errors.Is(err, sc.ErrPolicyLocked) {
		t.Fatalf("save while running = %v, want ErrPolicyLocked", err)
	}
	if err := configurator.Remove(c); !errors.Is(err, sc.ErrPolicyLocked) {
		t.Fatalf("remove while running = %v, want ErrPolicyLocked", err)
	}
}

func TestAnInvalidPolicyIsRefused(t *testing.T) {
	s := newStore()
	c := s.addCampaign("c1", 10, nil)
	policy := sc.PresetPolicy(sc.Standard, nil)

	if err := newConfigurator(s, nil, nil).Save(c, policy); !errors.Is(err, sc.ErrInvalidPolicy) {
		t.Fatalf("save without send hours = %v, want ErrInvalidPolicy", err)
	}
	if _, ok := s.policies["c1"]; ok {
		t.Fatal("an invalid policy was stored")
	}
}

func TestAValidPolicyIsStoredAndCanBeRemovedBeforeRunning(t *testing.T) {
	s := newStore()
	c := s.addCampaign("c1", 10, nil)
	configurator := newConfigurator(s, sendHours(), nil)

	if err := configurator.Save(c, sc.PresetPolicy(sc.Conservative, sendHours())); err != nil {
		t.Fatal(err)
	}
	if s.policies["c1"].Preset != sc.Conservative {
		t.Fatalf("stored policy = %+v", s.policies["c1"])
	}
	c.Status = campaign.StatusPaused
	if err := configurator.Remove(c); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.policies["c1"]; ok {
		t.Fatal("the policy was not removed")
	}
}

func TestDefaultsPrefillTheDepartmentHoursOverTheWorkspace(t *testing.T) {
	s := newStore()
	c := s.addCampaign("c1", 10, nil)
	c.DepartmentID = "sales"

	defaults, err := newConfigurator(s, sendHours(), saturdayMornings()).Defaults(ctx, c)

	if err != nil || defaults.SendHours == nil || len(defaults.SendHours.Days["sat"]) != 1 || len(defaults.SendHours.Days["mon"]) != 0 {
		t.Fatalf("defaults = %+v, %v, want the department hours", defaults, err)
	}
	if len(defaults.Presets) != len(sc.Presets) || defaults.Presets[sc.Aggressive].FirstWaveSize != 500 || defaults.Presets[sc.Aggressive].SendHours != defaults.SendHours {
		t.Fatalf("presets = %+v, want every preset with the prefilled hours", defaults.Presets)
	}
}

func TestDefaultsFallBackToTheWorkspaceHours(t *testing.T) {
	s := newStore()
	c := s.addCampaign("c1", 10, nil)

	defaults, err := newConfigurator(s, sendHours(), nil).Defaults(ctx, c)

	if err != nil || defaults.SendHours == nil || len(defaults.SendHours.Days["mon"]) != 1 {
		t.Fatalf("defaults = %+v, %v, want the workspace hours", defaults, err)
	}
}
