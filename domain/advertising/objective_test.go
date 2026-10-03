package advertising

import "testing"

func TestPostEngagementNoLongerOptimizesForImpressions(t *testing.T) {
	if ObjectiveEngagement.Allows(DestinationOnPost, GoalImpressions) {
		t.Fatal("meta deprecated IMPRESSIONS on ON_POST in v20")
	}
	for _, goal := range []OptimizationGoal{GoalPostEngagement, GoalReach} {
		if !ObjectiveEngagement.Allows(DestinationOnPost, goal) {
			t.Fatalf("post engagement refuses %s", goal)
		}
	}
}

func TestTrafficToWhatsAppOptimizesForClicksReachOrImpressions(t *testing.T) {
	for _, goal := range []OptimizationGoal{GoalLinkClicks, GoalReach, GoalImpressions} {
		if !ObjectiveTraffic.Allows(DestinationWhatsApp, goal) {
			t.Fatalf("traffic to whatsapp refuses %s", goal)
		}
	}
	if ObjectiveTraffic.Allows(DestinationWhatsApp, GoalLandingPageViews) {
		t.Fatal("whatsapp has no landing page to view")
	}
}

func TestMetaDestinationTypeFollowsTheObjectiveTable(t *testing.T) {
	cases := map[Destination]string{
		DestinationOnPost: "ON_POST", DestinationApp: "", DestinationNone: "", DestinationCatalog: "",
		DestinationWhatsApp: "WHATSAPP", DestinationMessenger: "MESSENGER", DestinationInstagramDirect: "INSTAGRAM_DIRECT",
		DestinationInstantForm: "ON_AD", DestinationWebsite: "WEBSITE",
	}
	for d, want := range cases {
		if got := d.MetaDestinationType(); got != want {
			t.Fatalf("%s sends %q, want %q", d, got, want)
		}
	}
}

func TestDestinationReadBackFromMeta(t *testing.T) {
	cases := []struct {
		objective Objective
		metaType  string
		goal      OptimizationGoal
		want      Destination
	}{
		{ObjectiveAppPromotion, "", GoalAppInstalls, DestinationApp},
		{ObjectiveAppPromotion, "UNDEFINED", GoalLinkClicks, DestinationApp},
		{ObjectiveSales, "", GoalOffsiteConversion, DestinationCatalog},
		{ObjectiveAwareness, "UNDEFINED", GoalReach, DestinationNone},
		{ObjectiveEngagement, "WHATSAPP", GoalConversations, DestinationWhatsApp},
		{ObjectiveEngagement, "ON_POST", GoalPostEngagement, DestinationOnPost},
		{ObjectiveTraffic, "", GoalLinkClicks, DestinationNone},
	}
	for _, c := range cases {
		if got := c.objective.DestinationOf(c.metaType, c.goal); got != c.want {
			t.Fatalf("%s %q %s read as %s, want %s", c.objective, c.metaType, c.goal, got, c.want)
		}
	}
}

func TestTheDefaultGoalIsTheFirstGoalOfTheRoute(t *testing.T) {
	cases := []struct {
		objective   Objective
		destination Destination
		want        OptimizationGoal
	}{
		{ObjectiveTraffic, DestinationWebsite, GoalLandingPageViews},
		{ObjectiveEngagement, DestinationWhatsApp, GoalConversations},
		{ObjectiveLeads, DestinationInstantForm, GoalLeadGeneration},
		{ObjectiveAwareness, DestinationNone, GoalReach},
	}
	for _, c := range cases {
		if got, ok := c.objective.DefaultGoal(c.destination); !ok || got != c.want {
			t.Errorf("%s %s: got %s", c.objective, c.destination, got)
		}
	}
	if _, ok := ObjectiveAwareness.DefaultGoal(DestinationWhatsApp); ok {
		t.Fatal("a destination the objective does not allow has no goal")
	}
}
