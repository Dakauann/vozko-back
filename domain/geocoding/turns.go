package geocoding

func NextTurn(after string, claims []Claim) string {
	var ahead, wrapped string
	for _, c := range claims {
		ws := c.WorkspaceID
		switch {
		case ws > after && ws > ahead:
			ahead = ws
		case ws <= after && ws > wrapped:
			wrapped = ws
		}
	}
	switch {
	case wrapped != "":
		return wrapped
	case ahead != "":
		return ahead
	}
	return after
}
