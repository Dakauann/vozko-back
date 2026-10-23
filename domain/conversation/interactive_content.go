package conversation

import (
	"bytes"
	"encoding/json"
	"strings"

	"vozko/domain/channel"
)

const (
	InteractiveMetadataKey      = "interactive"
	InteractiveReplyMetadataKey = "interactive_reply"
	DefaultListButtonLabel      = "Ver opções"
)

var interactiveMetadataMarker = []byte(`"` + InteractiveMetadataKey + `"`)

type InteractiveKind string

const (
	InteractiveKindButtons  InteractiveKind = "buttons"
	InteractiveKindList     InteractiveKind = "list"
	InteractiveKindCTAURL   InteractiveKind = "cta_url"
	InteractiveKindTemplate InteractiveKind = "template"
)

func (k InteractiveKind) Valid() bool {
	switch k {
	case InteractiveKindButtons, InteractiveKindList, InteractiveKindCTAURL, InteractiveKindTemplate:
		return true
	}
	return false
}

type InteractiveActionKind string

const (
	InteractiveActionReply    InteractiveActionKind = "reply"
	InteractiveActionURL      InteractiveActionKind = "url"
	InteractiveActionPhone    InteractiveActionKind = "phone"
	InteractiveActionCopyCode InteractiveActionKind = "copy_code"
	InteractiveActionFlow     InteractiveActionKind = "flow"
)

type InteractiveHeader struct {
	Type     string `json:"type" example:"text" enums:"text,image,video,document"`
	Text     string `json:"text,omitempty" example:"Atendimento"`
	MediaURL string `json:"media_url,omitempty" example:"https://cdn.exemplo.com/banner.jpg"`
}

type InteractiveRow struct {
	ID          string `json:"id" example:"agendar"`
	Title       string `json:"title" example:"Agendar consulta"`
	Description string `json:"description,omitempty" example:"Escolha dia e horário"`
}

type InteractiveSection struct {
	Title string           `json:"title,omitempty" example:"Assuntos"`
	Rows  []InteractiveRow `json:"rows"`
}

type InteractiveAction struct {
	Kind  InteractiveActionKind `json:"kind" example:"reply" enums:"reply,url,phone,copy_code,flow"`
	ID    string                `json:"id,omitempty" example:"sim"`
	Title string                `json:"title" example:"Sim"`
	URL   string                `json:"url,omitempty" example:"https://exemplo.com/pedido"`
	Phone string                `json:"phone,omitempty" example:"+5511999999999"`
	Code  string                `json:"code,omitempty" example:"PROMO10"`
}

type InteractiveContent struct {
	Kind         InteractiveKind      `json:"kind" example:"list" enums:"buttons,list,cta_url,template"`
	Header       *InteractiveHeader   `json:"header,omitempty"`
	Body         string               `json:"body" example:"Selecione alguma das opções abaixo"`
	Footer       string               `json:"footer,omitempty" example:"Atendimento 24h"`
	ButtonLabel  string               `json:"button_label,omitempty" example:"Ver opções"`
	Sections     []InteractiveSection `json:"sections,omitempty"`
	Buttons      []InteractiveAction  `json:"buttons,omitempty"`
	TemplateName string               `json:"template_name,omitempty" example:"boas_vindas"`
}

func (c InteractiveContent) usable() bool {
	if !c.Kind.Valid() {
		return false
	}
	switch c.Kind {
	case InteractiveKindList:
		return len(c.Sections) > 0 && strings.TrimSpace(c.ButtonLabel) != ""
	case InteractiveKindTemplate:
		return true
	}
	return len(c.Buttons) > 0
}

func (c InteractiveContent) OptionIDs() []string {
	ids := []string{}
	for _, section := range c.Sections {
		for _, row := range section.Rows {
			ids = append(ids, row.ID)
		}
	}
	for _, action := range c.Buttons {
		if action.Kind == InteractiveActionReply && action.ID != "" {
			ids = append(ids, action.ID)
		}
	}
	return ids
}

type InteractiveReply struct {
	Kind     string `json:"kind" example:"list"`
	OptionID string `json:"option_id" example:"agendar"`
	Title    string `json:"title,omitempty" example:"Agendar consulta"`
}

func (r *InteractiveReply) usable() bool {
	return r != nil && strings.TrimSpace(r.OptionID) != ""
}

func ListButtonLabel(label string) string {
	if trimmed := strings.TrimSpace(label); trimmed != "" {
		return trimmed
	}
	return DefaultListButtonLabel
}

func (r SendInteractiveRequest) ListButtonLabel() string {
	return ListButtonLabel(r.ListButton)
}

func (r SendInteractiveRequest) Composed() SendInteractiveRequest {
	r.Body = r.ComposedBody()
	r.Header, r.Footer = "", ""
	return r
}

func (r SendInteractiveRequest) WithHeaderInBody() SendInteractiveRequest {
	parts := make([]string, 0, 2)
	for _, part := range []string{r.Header, r.Body} {
		if p := strings.TrimSpace(part); p != "" {
			parts = append(parts, p)
		}
	}
	r.Body = strings.Join(parts, "\n\n")
	r.Header = ""
	return r
}

func (r SendInteractiveRequest) ShowingAs(style string, shown []InteractiveOption) InteractiveContent {
	content := InteractiveContent{
		Kind:   InteractiveKindButtons,
		Header: textHeader(r.Header),
		Body:   strings.TrimSpace(r.Body),
		Footer: strings.TrimSpace(r.Footer),
	}
	if style != channel.InteractiveStyleList {
		for _, opt := range shown {
			content.Buttons = append(content.Buttons, InteractiveAction{Kind: InteractiveActionReply, ID: opt.ID, Title: opt.Title})
		}
		return content
	}
	content.Kind = InteractiveKindList
	content.ButtonLabel = r.ListButtonLabel()
	for i, opt := range shown {
		if i == 0 || opt.Section != shown[i-1].Section {
			content.Sections = append(content.Sections, InteractiveSection{Title: strings.TrimSpace(opt.Section)})
		}
		last := &content.Sections[len(content.Sections)-1]
		last.Rows = append(last.Rows, InteractiveRow{ID: opt.ID, Title: opt.Title, Description: strings.TrimSpace(opt.Description)})
	}
	return content
}

func textHeader(text string) *InteractiveHeader {
	if t := strings.TrimSpace(text); t != "" {
		return &InteractiveHeader{Type: string(HeaderTypeText), Text: t}
	}
	return nil
}

func ButtonMessageContent(in SendButtonMessageInput) InteractiveContent {
	content := InteractiveContent{
		Kind:   InteractiveKindButtons,
		Header: buttonMessageHeader(in),
		Body:   strings.TrimSpace(in.BodyText),
		Footer: strings.TrimSpace(in.FooterText),
	}
	for _, b := range in.Buttons {
		if b.Type == ButtonTypeCopyCode {
			content.Buttons = append(content.Buttons, InteractiveAction{Kind: InteractiveActionCopyCode, Code: b.CopyCode})
			continue
		}
		content.Buttons = append(content.Buttons, InteractiveAction{Kind: InteractiveActionReply, ID: b.ID, Title: b.Title})
	}
	return content
}

func buttonMessageHeader(in SendButtonMessageInput) *InteractiveHeader {
	switch in.HeaderType {
	case HeaderTypeImage, HeaderTypeVideo:
		return &InteractiveHeader{Type: string(in.HeaderType), MediaURL: strings.TrimSpace(in.MediaLink)}
	}
	return textHeader(in.HeaderText)
}

func ListMessageContent(in SendListMessageInput) InteractiveContent {
	content := InteractiveContent{
		Kind:        InteractiveKindList,
		Header:      textHeader(in.HeaderText),
		Body:        strings.TrimSpace(in.BodyText),
		Footer:      strings.TrimSpace(in.FooterText),
		ButtonLabel: strings.TrimSpace(in.ButtonText),
	}
	for _, section := range in.Sections {
		rows := make([]InteractiveRow, 0, len(section.Rows))
		for _, row := range section.Rows {
			rows = append(rows, InteractiveRow{ID: row.ID, Title: row.Title, Description: strings.TrimSpace(row.Description)})
		}
		content.Sections = append(content.Sections, InteractiveSection{Title: strings.TrimSpace(section.Title), Rows: rows})
	}
	return content
}

func ButtonInputContent(in SendButtonInput) InteractiveContent {
	content := InteractiveContent{
		Kind:   InteractiveKindButtons,
		Body:   strings.TrimSpace(in.BodyText),
		Footer: strings.TrimSpace(in.FooterText),
	}
	if text := strings.TrimSpace(in.HeaderText); text != "" {
		headerType := strings.TrimSpace(in.HeaderType)
		if headerType == "" {
			headerType = string(HeaderTypeText)
		}
		content.Header = &InteractiveHeader{Type: headerType, Text: text}
	}
	for _, b := range in.Buttons {
		content.Buttons = append(content.Buttons, InteractiveAction{Kind: InteractiveActionReply, ID: b.ID, Title: b.Title})
	}
	return content
}

func (c InteractiveContent) buttonInput() (SendButtonInput, bool) {
	if c.Kind != InteractiveKindButtons || len(c.Buttons) == 0 {
		return SendButtonInput{}, false
	}
	in := SendButtonInput{BodyText: c.Body, FooterText: c.Footer}
	if c.Header != nil {
		in.HeaderType, in.HeaderText = c.Header.Type, c.Header.Text
	}
	for _, action := range c.Buttons {
		if action.Kind != InteractiveActionReply {
			return SendButtonInput{}, false
		}
		in.Buttons = append(in.Buttons, ButtonPayload{ID: action.ID, Title: action.Title})
	}
	return in, true
}

func ShowingOptions(out *SendOutcome, shown InteractiveContent) *SendOutcome {
	if out == nil {
		return nil
	}
	out.ShownOptionIDs = shown.OptionIDs()
	out.Interactive = &shown
	return out
}

func (m *Message) AttachInteractive(content *InteractiveContent) error {
	if content == nil || !content.usable() {
		return nil
	}
	return m.AttachMetadata(InteractiveMetadataKey, content)
}

func (m *Message) AttachInteractiveReply(reply *InteractiveReply) error {
	if !reply.usable() {
		return nil
	}
	return m.AttachMetadata(InteractiveReplyMetadataKey, reply)
}

func (m *Message) storedInteractive() (InteractiveContent, bool) {
	if m == nil || !bytes.Contains(m.Metadata, interactiveMetadataMarker) {
		return InteractiveContent{}, false
	}
	stored, ok := metadataFields(m.Metadata)[InteractiveMetadataKey]
	if !ok {
		return InteractiveContent{}, false
	}
	var content InteractiveContent
	if err := json.Unmarshal(stored, &content); err != nil || !content.usable() {
		return InteractiveContent{}, false
	}
	return content, true
}

func (m *Message) Interactive() (InteractiveContent, bool) {
	if m == nil || !m.ResolvedDirection().IsOutbound() {
		return InteractiveContent{}, false
	}
	if content, ok := m.storedInteractive(); ok {
		return content, true
	}
	if content, ok := m.templateInteractive(); ok {
		return content, true
	}
	if list, ok := m.List(); ok {
		return legacyListContent(list), true
	}
	if buttons, ok := m.legacyButtons(); ok {
		return ButtonInputContent(buttons), true
	}
	return InteractiveContent{}, false
}

func legacyListContent(list ListPayload) InteractiveContent {
	content := InteractiveContent{
		Kind:        InteractiveKindList,
		Header:      textHeader(list.HeaderText),
		Body:        list.BodyText,
		Footer:      list.FooterText,
		ButtonLabel: list.ButtonText,
	}
	if strings.TrimSpace(content.ButtonLabel) == "" {
		content.ButtonLabel = DefaultListButtonLabel
	}
	for _, section := range list.Sections {
		rows := make([]InteractiveRow, 0, len(section.Rows))
		for _, row := range section.Rows {
			rows = append(rows, InteractiveRow(row))
		}
		content.Sections = append(content.Sections, InteractiveSection{Title: section.Title, Rows: rows})
	}
	return content
}

func (m *Message) InteractiveReply() (InteractiveReply, bool) {
	if m == nil || m.ResolvedDirection().IsOutbound() {
		return InteractiveReply{}, false
	}
	fields := metadataFields(m.Metadata)
	if fields == nil {
		return InteractiveReply{}, false
	}
	if stored, ok := fields[InteractiveReplyMetadataKey]; ok {
		var reply InteractiveReply
		if err := json.Unmarshal(stored, &reply); err == nil && reply.usable() {
			return reply, true
		}
	}
	return legacyInteractiveReply(fields, m.Text)
}

func legacyInteractiveReply(fields map[string]json.RawMessage, text string) (InteractiveReply, bool) {
	var choice struct {
		Type  string `json:"type"`
		ID    string `json:"id"`
		Title string `json:"title"`
	}
	if raw, ok := fields[InteractiveMetadataKey]; ok && json.Unmarshal(raw, &choice) == nil && strings.TrimSpace(choice.ID) != "" {
		return InteractiveReply{Kind: legacyReplyKind(choice.Type), OptionID: choice.ID, Title: choice.Title}, true
	}
	var button struct {
		Payload string `json:"payload"`
		Text    string `json:"text"`
	}
	if raw, ok := fields["button"]; ok && json.Unmarshal(raw, &button) == nil && strings.TrimSpace(button.Payload) != "" {
		return InteractiveReply{Kind: InteractiveReplyTemplateButton, OptionID: button.Payload, Title: button.Text}, true
	}
	if id := strings.TrimSpace(metadataString(fields, "selectedOptionId")); id != "" {
		return InteractiveReply{Kind: InteractiveReplyOption, OptionID: id, Title: strings.TrimSpace(text)}, true
	}
	return InteractiveReply{}, false
}

const (
	InteractiveReplyButton         = "button"
	InteractiveReplyList           = "list"
	InteractiveReplyTemplateButton = "template_button"
	InteractiveReplyOption         = "option"
	InteractiveReplyQuickReply     = "quick_reply"
)

func legacyReplyKind(providerType string) string {
	switch providerType {
	case "list_reply":
		return InteractiveReplyList
	case "button_reply":
		return InteractiveReplyButton
	}
	return InteractiveReplyOption
}

type storedTemplate struct {
	TemplateName   string `json:"template_name"`
	BodyText       string `json:"body_text"`
	HeaderMediaURL string `json:"header_media_url"`
	Components     []struct {
		Type    string `json:"type"`
		Format  string `json:"format"`
		Text    string `json:"text"`
		Buttons []struct {
			Type        string `json:"type"`
			Text        string `json:"text"`
			URL         string `json:"url"`
			PhoneNumber string `json:"phoneNumber"`
		} `json:"buttons"`
	} `json:"components"`
}

func (m *Message) templateInteractive() (InteractiveContent, bool) {
	if m.MessageType != MessageTypeTemplate || len(bytes.TrimSpace(m.Metadata)) == 0 {
		return InteractiveContent{}, false
	}
	var stored storedTemplate
	if err := json.Unmarshal(m.Metadata, &stored); err != nil {
		return InteractiveContent{}, false
	}
	if strings.TrimSpace(stored.TemplateName) == "" && len(stored.Components) == 0 {
		return InteractiveContent{}, false
	}
	content := InteractiveContent{Kind: InteractiveKindTemplate, TemplateName: strings.TrimSpace(stored.TemplateName)}
	for _, component := range stored.Components {
		switch strings.ToUpper(component.Type) {
		case "HEADER":
			content.Header = templateHeader(component.Format, component.Text, stored.HeaderMediaURL)
		case "BODY":
			content.Body = strings.TrimSpace(component.Text)
		case "FOOTER":
			content.Footer = strings.TrimSpace(component.Text)
		case "BUTTONS":
			for _, button := range component.Buttons {
				content.Buttons = append(content.Buttons, templateAction(button.Type, button.Text, button.URL, button.PhoneNumber))
			}
		}
	}
	if content.Body == "" {
		content.Body = strings.TrimSpace(stored.BodyText)
	}
	if content.Body == "" {
		content.Body = m.Text
	}
	return content, true
}

func templateHeader(format, text, mediaURL string) *InteractiveHeader {
	switch strings.ToUpper(format) {
	case "IMAGE", "VIDEO", "DOCUMENT":
		return &InteractiveHeader{Type: strings.ToLower(format), MediaURL: strings.TrimSpace(mediaURL)}
	}
	return textHeader(text)
}

func templateAction(buttonType, text, url, phone string) InteractiveAction {
	action := InteractiveAction{Kind: InteractiveActionReply, Title: strings.TrimSpace(text)}
	switch strings.ToUpper(buttonType) {
	case "URL":
		action.Kind = InteractiveActionURL
		if link := strings.TrimSpace(url); !strings.Contains(link, "{{") {
			action.URL = link
		}
	case "PHONE_NUMBER":
		action.Kind, action.Phone = InteractiveActionPhone, strings.TrimSpace(phone)
	case "COPY_CODE", "OTP":
		action.Kind = InteractiveActionCopyCode
	case "FLOW":
		action.Kind = InteractiveActionFlow
	}
	return action
}
