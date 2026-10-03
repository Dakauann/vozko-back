package advertisinghttp

import (
	"reflect"
	"testing"

	"vozko/domain/advertising"
)

func TestOptionsListEveryObjectiveWithTheRoutesItAllows(t *testing.T) {
	options := buildOptions()
	if len(options.Objectives) != len(advertising.AllObjectives()) {
		t.Fatalf("got %d objectives", len(options.Objectives))
	}
	for _, o := range options.Objectives {
		if len(o.Routes) == 0 {
			t.Fatalf("%s has no routes", o.Objective)
		}
		for _, route := range o.Routes {
			if len(route.Goals) == 0 {
				t.Fatalf("%s %s has no goals", o.Objective, route.Destination)
			}
			for _, goal := range route.Goals {
				if !o.Objective.Allows(route.Destination, goal) {
					t.Fatalf("%s offers %s/%s that the domain refuses", o.Objective, route.Destination, goal)
				}
			}
		}
	}
}

func TestOptionsComeFromTheDomainLists(t *testing.T) {
	options := buildOptions()
	checks := map[string][2]any{
		"callsToAction":      {options.CallsToAction, advertising.LinkCallsToAction()},
		"placements":         {options.Placements, advertising.PlatformPositions()},
		"breakdownGroups":    {options.BreakdownGroups, advertising.BreakdownGroups()},
		"attributionWindows": {options.AttributionWindows, advertising.AttributionWindows()},
		"matchKeys":          {options.MatchKeys, advertising.AllMatchKeys()},
		"ruleMetrics":        {options.RuleMetrics, advertising.RuleMetrics()},
		"pixelEvents":        {options.PixelEvents, advertising.PixelEvents()},
		"formats":            {options.Formats, advertising.CreativeFormats()},
	}
	for name, pair := range checks {
		if !reflect.DeepEqual(pair[0], pair[1]) {
			t.Errorf("%s: got %v, want %v", name, pair[0], pair[1])
		}
		if reflect.ValueOf(pair[0]).Len() == 0 {
			t.Errorf("%s is empty", name)
		}
	}
}

func TestOptionsDoNotShareTheDomainRoutes(t *testing.T) {
	first := buildOptions()
	first.Objectives[0].Routes[0].Goals[0] = "CHANGED"
	if buildOptions().Objectives[0].Routes[0].Goals[0] == "CHANGED" {
		t.Fatal("changing a response changed the objective matrix")
	}
}
