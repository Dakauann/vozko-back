package copilot

import (
	"errors"
	"regexp"

	"vozko/domain/workspace"
)

var ErrInvalidDestination = errors.New("invalid destination")

var destinationParam = regexp.MustCompile(`^[A-Za-z0-9_-]{1,100}$`)

type Destination struct {
	Screen workspace.Screen  `json:"screen"`
	Params map[string]string `json:"params,omitempty"`
}

func NewNavigationCard(screen workspace.Screen, id string) (*ActionCard, error) {
	if !screen.Valid() {
		return nil, ErrInvalidDestination
	}
	destination := &Destination{Screen: screen}
	switch params := screen.Params(); {
	case len(params) == 0 && id == "":
	case len(params) == 1 && destinationParam.MatchString(id):
		destination.Params = map[string]string{params[0]: id}
	default:
		return nil, ErrInvalidDestination
	}
	return &ActionCard{Kind: ActionOpenScreen, Destination: destination}, nil
}
