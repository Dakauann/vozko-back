package workspace

import "testing"

var harmlessDeletions = map[Resource]bool{
	ResourceAIChat:            true,
	ResourceCalendar:          true,
	ResourceAttendanceTargets: true,
}

func TestCatalogRisksUseKnownKinds(t *testing.T) {
	used := map[RiskKind]bool{}
	for resource, actions := range ResourceActions {
		for _, def := range actions {
			for _, k := range def.Risks {
				if _, known := riskLevels[k]; !known {
					t.Errorf("%s:%s uses unknown risk kind %q", resource, def.ActionName, k)
				}
				used[k] = true
			}
		}
	}
	for k := range riskLevels {
		if !used[k] {
			t.Errorf("risk kind %q is defined but no permission carries it", k)
		}
	}
}

func TestDeletingSharedDataIsFlagged(t *testing.T) {
	for resource, actions := range ResourceActions {
		for _, def := range actions {
			if def.ActionName == ActionDelete && !harmlessDeletions[resource] && len(def.Risks) == 0 {
				t.Errorf("%s:delete has no risk; flag it or add the resource to harmlessDeletions", resource)
			}
		}
	}
}

func TestChangingAccessIsFlagged(t *testing.T) {
	for _, resource := range []Resource{ResourceMembers, ResourceRoles, ResourceAssignments} {
		for _, def := range ResourceActions[resource] {
			if def.ActionName == ActionRead || def.ActionName == ActionViewOthers {
				continue
			}
			if !containsRisk(def.Risks, RiskManagesAccess) {
				t.Errorf("%s:%s changes access but is not flagged", resource, def.ActionName)
			}
		}
	}
}

func containsRisk(kinds []RiskKind, want RiskKind) bool {
	for _, k := range kinds {
		if k == want {
			return true
		}
	}
	return false
}

func TestRisksOfAPermission(t *testing.T) {
	start := PermissionEntry{Resource: ResourceWhatsAppCampaigns, Action: ActionStart}
	risks := RisksOf(start)
	if len(risks) != 2 || risks[0].Kind != RiskSpendsBalance || risks[0].Level != RiskHigh || risks[0].Description == "" {
		t.Fatalf("risks = %+v", risks)
	}
	if RisksOf(PermissionEntry{Resource: ResourceLabels, Action: ActionRead}) != nil {
		t.Fatal("reading labels was flagged")
	}
}

func TestRiskyPermissionsKeepsOnlyTheFlaggedOnesSorted(t *testing.T) {
	flagged := RiskyPermissions([]PermissionEntry{
		{Resource: ResourceLabels, Action: ActionRead},
		{Resource: ResourceMembers, Action: ActionUpdate},
		{Resource: ResourceCallRecordings, Action: ActionRead},
	})
	if len(flagged) != 2 || flagged[0].Permission.Resource != ResourceCallRecordings || flagged[1].Level() != RiskHigh {
		t.Fatalf("flagged = %+v", flagged)
	}
}
