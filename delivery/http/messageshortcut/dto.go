package messageshortcut

import (
	messageshortcutdomain "vozko/domain/message_shortcut"
)

type CreateShortcutRequest struct {
	Name        string          `json:"name" example:"saudacao"`
	Shortcut    string          `json:"shortcut" example:"/ola"`
	MessageType string          `json:"messageType" example:"text"`
	Content     ShortcutContent `json:"content"`
}

type UpdateShortcutRequest struct {
	Name        *string          `json:"name,omitempty" example:"saudacao"`
	Shortcut    *string          `json:"shortcut,omitempty" example:"/ola"`
	MessageType *string          `json:"messageType,omitempty" example:"text"`
	Content     *ShortcutContent `json:"content,omitempty"`
}

type ShortcutContent struct {
	Text       string           `json:"text,omitempty"`
	HeaderType string           `json:"headerType,omitempty"`
	HeaderText string           `json:"headerText,omitempty"`
	FooterText string           `json:"footerText,omitempty"`
	MediaURL   string           `json:"mediaUrl,omitempty"`
	MediaType  string           `json:"mediaType,omitempty"`
	Buttons    []ShortcutButton `json:"buttons,omitempty"`
}

type ShortcutButton struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

func (c ShortcutContent) toDomain() messageshortcutdomain.Content {
	buttons := make([]messageshortcutdomain.Button, len(c.Buttons))
	for idx, button := range c.Buttons {
		buttons[idx] = messageshortcutdomain.Button{ID: button.ID, Title: button.Title}
	}
	return messageshortcutdomain.Content{
		Text: c.Text, HeaderType: c.HeaderType, HeaderText: c.HeaderText,
		FooterText: c.FooterText, MediaURL: c.MediaURL, MediaType: c.MediaType,
		Buttons: buttons,
	}
}

func toMessageType(value *string) *messageshortcutdomain.MessageType {
	if value == nil {
		return nil
	}
	converted := messageshortcutdomain.MessageType(*value)
	return &converted
}

func toShortcutContent(value *ShortcutContent) *messageshortcutdomain.Content {
	if value == nil {
		return nil
	}
	converted := value.toDomain()
	return &converted
}
