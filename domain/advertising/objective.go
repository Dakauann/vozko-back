package advertising

import "slices"

type Objective string

const (
	ObjectiveAwareness    Objective = "OUTCOME_AWARENESS"
	ObjectiveTraffic      Objective = "OUTCOME_TRAFFIC"
	ObjectiveEngagement   Objective = "OUTCOME_ENGAGEMENT"
	ObjectiveLeads        Objective = "OUTCOME_LEADS"
	ObjectiveSales        Objective = "OUTCOME_SALES"
	ObjectiveAppPromotion Objective = "OUTCOME_APP_PROMOTION"
)

func AllObjectives() []Objective {
	return []Objective{ObjectiveAwareness, ObjectiveTraffic, ObjectiveEngagement, ObjectiveLeads, ObjectiveSales, ObjectiveAppPromotion}
}

const (
	DestinationWebsite     Destination = "WEBSITE"
	DestinationInstantForm Destination = "ON_AD"
	DestinationApp         Destination = "APP"
	DestinationOnPost      Destination = "ON_POST"
	DestinationNone        Destination = "NONE"
	DestinationCatalog     Destination = "CATALOG"
)

type OptimizationGoal string

const (
	GoalReach             OptimizationGoal = "REACH"
	GoalImpressions       OptimizationGoal = "IMPRESSIONS"
	GoalAdRecallLift      OptimizationGoal = "AD_RECALL_LIFT"
	GoalThruPlay          OptimizationGoal = "THRUPLAY"
	GoalLinkClicks        OptimizationGoal = "LINK_CLICKS"
	GoalLandingPageViews  OptimizationGoal = "LANDING_PAGE_VIEWS"
	GoalPostEngagement    OptimizationGoal = "POST_ENGAGEMENT"
	GoalMessaging         OptimizationGoal = GoalConversations
	GoalLeadGeneration    OptimizationGoal = "LEAD_GENERATION"
	GoalQualityLead       OptimizationGoal = "QUALITY_LEAD"
	GoalOffsiteConversion OptimizationGoal = "OFFSITE_CONVERSIONS"
	GoalValue             OptimizationGoal = "VALUE"
	GoalAppInstalls       OptimizationGoal = "APP_INSTALLS"
)

type PixelEvent string

const (
	EventPurchase             PixelEvent = "PURCHASE"
	EventLead                 PixelEvent = "LEAD"
	EventCompleteRegistration PixelEvent = "COMPLETE_REGISTRATION"
	EventAddToCart            PixelEvent = "ADD_TO_CART"
	EventInitiateCheckout     PixelEvent = "INITIATE_CHECKOUT"
	EventContact              PixelEvent = "CONTACT"
	EventSchedule             PixelEvent = "SCHEDULE"
	EventSubscribe            PixelEvent = "SUBSCRIBE"
)

var pixelEvents = []PixelEvent{EventPurchase, EventLead, EventCompleteRegistration, EventAddToCart, EventInitiateCheckout, EventContact, EventSchedule, EventSubscribe}

func PixelEvents() []PixelEvent { return append([]PixelEvent(nil), pixelEvents...) }

func (e PixelEvent) Valid() bool { return slices.Contains(pixelEvents, e) }

type Route struct {
	Destination Destination
	Goals       []OptimizationGoal
}

var messagingDestinations = []Destination{DestinationWhatsApp, DestinationMessenger, DestinationInstagramDirect}

func messagingRoutes(goals ...OptimizationGoal) []Route {
	routes := make([]Route, 0, len(messagingDestinations))
	for _, d := range messagingDestinations {
		routes = append(routes, Route{Destination: d, Goals: goals})
	}
	return routes
}

var objectiveRoutes = map[Objective][]Route{
	ObjectiveAwareness: {
		{Destination: DestinationNone, Goals: []OptimizationGoal{GoalReach, GoalImpressions, GoalAdRecallLift, GoalThruPlay}},
	},
	ObjectiveTraffic: {
		{Destination: DestinationWebsite, Goals: []OptimizationGoal{GoalLandingPageViews, GoalLinkClicks, GoalReach, GoalImpressions}},
		{Destination: DestinationWhatsApp, Goals: []OptimizationGoal{GoalLinkClicks, GoalReach, GoalImpressions}},
		{Destination: DestinationMessenger, Goals: []OptimizationGoal{GoalLinkClicks, GoalReach, GoalImpressions}},
	},
	ObjectiveEngagement: append([]Route{
		{Destination: DestinationOnPost, Goals: []OptimizationGoal{GoalPostEngagement, GoalReach, GoalImpressions}},
		{Destination: DestinationWebsite, Goals: []OptimizationGoal{GoalOffsiteConversion, GoalLandingPageViews, GoalLinkClicks}},
	}, messagingRoutes(GoalMessaging, GoalLinkClicks)...),
	ObjectiveLeads: {
		{Destination: DestinationInstantForm, Goals: []OptimizationGoal{GoalLeadGeneration, GoalQualityLead}},
		{Destination: DestinationWebsite, Goals: []OptimizationGoal{GoalOffsiteConversion, GoalLinkClicks}},
		{Destination: DestinationWhatsApp, Goals: []OptimizationGoal{GoalMessaging}},
	},
	ObjectiveSales: {
		{Destination: DestinationWebsite, Goals: []OptimizationGoal{GoalOffsiteConversion, GoalValue, GoalLandingPageViews, GoalLinkClicks}},
		{Destination: DestinationCatalog, Goals: []OptimizationGoal{GoalOffsiteConversion, GoalValue}},
		{Destination: DestinationWhatsApp, Goals: []OptimizationGoal{GoalMessaging}},
		{Destination: DestinationMessenger, Goals: []OptimizationGoal{GoalMessaging}},
	},
	ObjectiveAppPromotion: {
		{Destination: DestinationApp, Goals: []OptimizationGoal{GoalAppInstalls, GoalLinkClicks}},
	},
}

func (o Objective) Valid() bool {
	_, ok := objectiveRoutes[o]
	return ok
}

func (o Objective) Routes() []Route { return objectiveRoutes[o] }

func (o Objective) Allows(d Destination, g OptimizationGoal) bool {
	for _, r := range objectiveRoutes[o] {
		if r.Destination == d {
			return slices.Contains(r.Goals, g)
		}
	}
	return false
}

func (d Destination) Messaging() bool { return slices.Contains(messagingDestinations, d) }

func (d Destination) MetaDestinationType() string {
	switch d {
	case DestinationNone, DestinationCatalog, DestinationOnPost:
		return ""
	}
	return string(d)
}

func (g OptimizationGoal) NeedsPixel() bool { return g == GoalOffsiteConversion || g == GoalValue }

func (g OptimizationGoal) BillingEvent() string { return BillingImpressions }

func (o Objective) AssetGroups() bool { return o == ObjectiveSales || o == ObjectiveAppPromotion }
