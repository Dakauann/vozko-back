package conversation

import "time"

type SendMessageRequest struct {
	Text             string  `json:"text" example:"Olá, como posso ajudar?"`
	MediaID          *string `json:"media_id,omitempty" example:"med_a1b2c3"`
	MediaType        *string `json:"media_type,omitempty" example:"image"`
	ReplyToMessageID string  `json:"reply_to_message_id,omitempty" example:"msg_a1b2c3"`
}

type CallPermissionRequestBody struct {
	BodyText string `json:"body_text,omitempty" example:"Podemos te ligar pelo WhatsApp?"`
}

type ReopenWindowRequest struct {
	TemplateID string   `json:"template_id" example:"tmpl_a1b2c3"`
	Parameters []string `json:"parameters,omitempty" example:"João,10/07"`
}

type MessageResponse struct {
	ID                string                    `json:"id"`
	EntryID           string                    `json:"entryId"`
	EntryType         string                    `json:"entryType"`
	Channel           string                    `json:"channel"`
	MessageType       string                    `json:"messageType"`
	From              string                    `json:"from"`
	To                string                    `json:"to"`
	Text              string                    `json:"text"`
	Image             []byte                    `json:"image,omitempty"`
	Video             []byte                    `json:"video,omitempty"`
	MediaID           *string                   `json:"mediaId,omitempty"`
	MediaType         string                    `json:"mediaType,omitempty"`
	Read              bool                      `json:"read"`
	ReadAt            *time.Time                `json:"readAt,omitempty"`
	ReadBy            *string                   `json:"readBy,omitempty"`
	WhatsAppMessageID *string                   `json:"whatsappMessageId,omitempty"`
	ReplyToMessageID  *string                   `json:"replyToMessageId,omitempty"`
	DeliveryStatus    string                    `json:"deliveryStatus,omitempty"`
	SenderName        string                    `json:"senderName,omitempty"`
	SenderAvatar      string                    `json:"senderAvatar,omitempty"`
	Metadata          map[string]interface{}    `json:"metadata,omitempty"`
	Location          *MessageLocationResponse  `json:"location,omitempty"`
	Interactive       *InteractiveResponse      `json:"interactive,omitempty"`
	InteractiveReply  *InteractiveReplyResponse `json:"interactive_reply,omitempty"`
	CreatedAt         time.Time                 `json:"createdAt"`
	UpdatedAt         time.Time                 `json:"updatedAt"`
}

type InteractiveResponse struct {
	Kind         string                       `json:"kind" example:"list" enums:"buttons,list,cta_url,template"`
	Header       *InteractiveHeaderResponse   `json:"header,omitempty"`
	Body         string                       `json:"body" example:"Selecione alguma das opções abaixo"`
	Footer       string                       `json:"footer,omitempty" example:"Atendimento 24h"`
	ButtonLabel  string                       `json:"button_label,omitempty" example:"Ver opções"`
	Sections     []InteractiveSectionResponse `json:"sections,omitempty"`
	Buttons      []InteractiveActionResponse  `json:"buttons,omitempty"`
	TemplateName string                       `json:"template_name,omitempty" example:"boas_vindas"`
}

type InteractiveHeaderResponse struct {
	Type     string `json:"type" example:"text" enums:"text,image,video,document"`
	Text     string `json:"text,omitempty" example:"Atendimento"`
	MediaURL string `json:"media_url,omitempty" example:"https://cdn.exemplo.com/banner.jpg"`
}

type InteractiveSectionResponse struct {
	Title string                   `json:"title,omitempty" example:"Assuntos"`
	Rows  []InteractiveRowResponse `json:"rows"`
}

type InteractiveRowResponse struct {
	ID          string `json:"id" example:"agendar"`
	Title       string `json:"title" example:"Agendar consulta"`
	Description string `json:"description,omitempty" example:"Escolha dia e horário"`
}

type InteractiveActionResponse struct {
	Kind  string `json:"kind" example:"reply" enums:"reply,url,phone,copy_code,flow"`
	ID    string `json:"id,omitempty" example:"sim"`
	Title string `json:"title" example:"Sim"`
	URL   string `json:"url,omitempty" example:"https://exemplo.com/pedido"`
	Phone string `json:"phone,omitempty" example:"+5511999999999"`
	Code  string `json:"code,omitempty" example:"PROMO10"`
}

type InteractiveReplyResponse struct {
	Kind     string `json:"kind" example:"list" enums:"button,list,template_button,option,quick_reply,postback"`
	OptionID string `json:"option_id" example:"agendar"`
	Title    string `json:"title,omitempty" example:"Agendar consulta"`
}

type MessageLocationResponse struct {
	Latitude  float64 `json:"latitude" example:"-23.55052"`
	Longitude float64 `json:"longitude" example:"-46.633308"`
	Name      string  `json:"name,omitempty" example:"Casa"`
	Address   string  `json:"address,omitempty" example:"Rua Augusta, 100"`
	Candidate bool    `json:"candidate"`
}

type MessageEnvelopeResponse struct {
	Message MessageResponse `json:"message"`
}

type SearchMessagesResponse struct {
	Messages   []MessageResponse `json:"messages"`
	Page       int               `json:"page"`
	PageSize   int               `json:"page_size"`
	TotalItems int64             `json:"total_items"`
	TotalPages int               `json:"total_pages"`
}
