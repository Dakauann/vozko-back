package copilot

import (
	"errors"
	"strings"
)

const (
	ActionEmailDesign ActionKind = "email_design"

	MaxEmailDesignCardNameRunes = 120
)

var ErrInvalidEmailDesignCard = errors.New("copilot: the email design card cannot be built")

type EmailDesignCard struct {
	DesignID string `json:"designId"`
	Name     string `json:"name"`
}

func NewEmailDesignCard(designID, name string) (*ActionCard, error) {
	clean := strings.TrimSpace(name)
	if !uuidPattern.MatchString(designID) || clean == "" || !plainText(clean, MaxEmailDesignCardNameRunes) {
		return nil, ErrInvalidEmailDesignCard
	}
	return &ActionCard{Kind: ActionEmailDesign, EmailDesign: &EmailDesignCard{DesignID: designID, Name: clean}}, nil
}
