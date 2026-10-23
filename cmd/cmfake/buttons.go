package main

import "encoding/json"

type button struct {
	Label    string `json:"label"`
	Postback string `json:"postback"`
	Action   string `json:"action"`
}

type wireSuggestion struct {
	Action       string `json:"action"`
	Label        string `json:"label"`
	PostbackData string `json:"postbackdata"`
}

type wireItem struct {
	Suggestions []wireSuggestion `json:"suggestions"`
	Carousel    *struct {
		Cards []wireItem `json:"cards"`
	} `json:"carousel"`
}

type wireSent struct {
	RichContent *struct {
		Conversation []wireItem       `json:"conversation"`
		Suggestions  []wireSuggestion `json:"suggestions"`
	} `json:"richContent"`
}

func buttonsOf(raw []byte) ([]button, error) {
	var msg wireSent
	if err := json.Unmarshal(raw, &msg); err != nil {
		return nil, err
	}
	if msg.RichContent == nil {
		return nil, nil
	}
	out := toButtons(nil, msg.RichContent.Suggestions)
	for _, item := range msg.RichContent.Conversation {
		out = toButtons(out, item.Suggestions)
		if item.Carousel != nil {
			for _, card := range item.Carousel.Cards {
				out = toButtons(out, card.Suggestions)
			}
		}
	}
	return out, nil
}

func toButtons(out []button, suggestions []wireSuggestion) []button {
	for _, s := range suggestions {
		out = append(out, button{Label: s.Label, Postback: s.PostbackData, Action: s.Action})
	}
	return out
}
