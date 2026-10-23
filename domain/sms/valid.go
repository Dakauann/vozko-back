package sms

func (k RouteKind) Valid() bool {
	return k == RouteShared || k == RouteDedicated
}

func (s RouteStatus) Valid() bool {
	return s == RouteActive || s == RoutePaused
}
