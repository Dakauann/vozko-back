package node_executors

import (
	"encoding/json"

	"vozko/domain/conversation"
)

func buttonChoices(input conversation.SendButtonMessageInput) conversation.SendButtonInput {
	choices := conversation.SendButtonInput{
		HeaderType: string(input.HeaderType),
		HeaderText: input.HeaderText,
		BodyText:   input.BodyText,
		FooterText: input.FooterText,
	}
	for _, b := range input.Buttons {
		choices.Buttons = append(choices.Buttons, conversation.ButtonPayload{ID: b.ID, Title: b.Title})
	}
	return choices
}

func listChoices(input conversation.SendListMessageInput) conversation.SendButtonInput {
	choices := conversation.SendButtonInput{
		HeaderText: input.HeaderText,
		BodyText:   input.BodyText,
		FooterText: input.FooterText,
	}
	for _, section := range input.Sections {
		for _, row := range section.Rows {
			choices.Buttons = append(choices.Buttons, conversation.ButtonPayload{ID: row.ID, Title: row.Title})
		}
	}
	return choices
}

func choicesMetadata(choices conversation.SendButtonInput) (json.RawMessage, error) {
	var carrier conversation.Message
	if err := carrier.AttachButtons(choices); err != nil {
		return nil, err
	}
	return carrier.Metadata, nil
}

func listMetadata(input conversation.SendListMessageInput) (json.RawMessage, error) {
	var carrier conversation.Message
	if err := carrier.AttachButtons(listChoices(input)); err != nil {
		return nil, err
	}
	if err := carrier.AttachList(input); err != nil {
		return nil, err
	}
	return carrier.Metadata, nil
}
