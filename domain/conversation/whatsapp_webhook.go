package conversation

type WhatsAppWebhookPayload struct {
	Object string          `json:"object"`
	Entry  []WhatsAppEntry `json:"entry"`
}

type WhatsAppEntry struct {
	ID      string           `json:"id"`
	Changes []WhatsAppChange `json:"changes"`
}

type WhatsAppChange struct {
	Value WhatsAppChangeValue `json:"value"`
	Field string              `json:"field"`
}

type WhatsAppChangeValue struct {
	MessagingProduct string            `json:"messaging_product"`
	Metadata         WhatsAppMetadata  `json:"metadata"`
	Contacts         []WhatsAppContact `json:"contacts"`
	Messages         []WhatsAppMessage `json:"messages"`
	Statuses         []WhatsAppStatus  `json:"statuses"`
	Errors           []WhatsAppError   `json:"errors"`
}

type WhatsAppMetadata struct {
	DisplayPhoneNumber string `json:"display_phone_number"`
	PhoneNumberID      string `json:"phone_number_id"`
}

type WhatsAppContact struct {
	WAID            string          `json:"wa_id"`
	Profile         WhatsAppProfile `json:"profile"`
	IdentityKeyHash string          `json:"identity_key_hash"`
}

type WhatsAppProfile struct {
	Name string `json:"name"`
}

type WhatsAppMessage struct {
	From        string                   `json:"from"`
	ID          string                   `json:"id"`
	Timestamp   string                   `json:"timestamp"`
	Type        string                   `json:"type"`
	Context     *WhatsAppContext         `json:"context,omitempty"`
	Referral    *WhatsAppReferral        `json:"referral,omitempty"`
	Reaction    *WhatsAppReaction        `json:"reaction,omitempty"`
	Order       *WhatsAppOrder           `json:"order,omitempty"`
	Contacts    []WhatsAppContactMessage `json:"contacts,omitempty"`
	Location    *WhatsAppLocation        `json:"location,omitempty"`
	Text        *WhatsAppTextMessage     `json:"text,omitempty"`
	Audio       *WhatsAppAudioMessage    `json:"audio,omitempty"`
	Image       *WhatsAppMediaMessage    `json:"image,omitempty"`
	Video       *WhatsAppMediaMessage    `json:"video,omitempty"`
	Document    *WhatsAppMediaMessage    `json:"document,omitempty"`
	Sticker     *WhatsAppStickerMessage  `json:"sticker,omitempty"`
	Interactive *WhatsAppInteractive     `json:"interactive,omitempty"`
	Button      *WhatsAppButton          `json:"button,omitempty"`
	Errors      []WhatsAppError          `json:"errors,omitempty"`
}

type WhatsAppContext struct {
	ID                  string `json:"id"`
	From                string `json:"from"`
	MessageID           string `json:"quoted_message_id"`
	Forwarded           bool   `json:"forwarded"`
	FrequentlyForwarded bool   `json:"frequently_forwarded"`
}

type WhatsAppReferral struct {
	SourceURL      string `json:"source_url"`
	SourceID       string `json:"source_id"`
	SourceType     string `json:"source_type"`
	Body           string `json:"body"`
	Headline       string `json:"headline"`
	MediaType      string `json:"media_type"`
	ImageURL       string `json:"image_url"`
	VideoURL       string `json:"video_url"`
	ThumbnailURL   string `json:"thumbnail_url"`
	CTWAClid       string `json:"ctwa_clid"`
	WelcomeMessage struct {
		Text string `json:"text"`
	} `json:"welcome_message"`
}

type WhatsAppReaction struct {
	MessageID string `json:"message_id"`
	Emoji     string `json:"emoji"`
}

type WhatsAppOrder struct {
	CatalogID string `json:"catalog_id"`
	Text      string `json:"text"`
}

type WhatsAppContactMessage struct {
	Name   WhatsAppContactName    `json:"name"`
	Phones []WhatsAppContactPhone `json:"phones"`
	Emails []WhatsAppContactEmail `json:"emails"`
}

type WhatsAppContactName struct {
	FormattedName string `json:"formatted_name"`
	FirstName     string `json:"first_name"`
	LastName      string `json:"last_name"`
}

type WhatsAppContactPhone struct {
	Phone string `json:"phone"`
	Type  string `json:"type"`
}

type WhatsAppContactEmail struct {
	Email string `json:"email"`
	Type  string `json:"type"`
}

type WhatsAppLocation struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Name      string  `json:"name"`
	Address   string  `json:"address"`
}

type WhatsAppTextMessage struct {
	Body string `json:"body"`
}

type WhatsAppAudioMessage struct {
	ID       string `json:"id"`
	MimeType string `json:"mime_type"`
	SHA256   string `json:"sha256"`
	URL      string `json:"url"`
	Voice    bool   `json:"voice"`
}

type WhatsAppMediaMessage struct {
	ID       string `json:"id"`
	Link     string `json:"link"`
	MimeType string `json:"mime_type"`
	SHA256   string `json:"sha256"`
	Caption  string `json:"caption"`
	FileName string `json:"filename"`
}

type WhatsAppStickerMessage struct {
	ID       string `json:"id"`
	Link     string `json:"link"`
	MimeType string `json:"mime_type"`
	SHA256   string `json:"sha256"`
	Animated bool   `json:"animated"`
}

type WhatsAppInteractive struct {
	Type                string                       `json:"type"`
	ButtonReply         *WhatsAppButtonReply         `json:"button_reply,omitempty"`
	ListReply           *WhatsAppListReply           `json:"list_reply,omitempty"`
	NFMReply            *WhatsAppNFMReply            `json:"nfm_reply,omitempty"`
	CallPermissionReply *WhatsAppCallPermissionReply `json:"call_permission_reply,omitempty"`
}

type WhatsAppCallPermissionReply struct {
	Response            string `json:"response"`
	ExpirationTimestamp int64  `json:"expiration_timestamp"`
	ResponseSource      string `json:"response_source"`
}

type WhatsAppNFMReply struct {
	ResponseJSON string `json:"response_json"`
	Body         string `json:"body"`
	Name         string `json:"name"`
}

type WhatsAppButton struct {
	Text    string `json:"text"`
	Payload string `json:"payload"`
}

type WhatsAppButtonReply struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type WhatsAppListReply struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

type WhatsAppStatus struct {
	ID           string                `json:"id"`
	Status       string                `json:"status"`
	Timestamp    string                `json:"timestamp"`
	RecipientID  string                `json:"recipient_id"`
	Conversation *WhatsAppConversation `json:"conversation,omitempty"`
	Pricing      *WhatsAppPricing      `json:"pricing,omitempty"`
	Errors       []WhatsAppError       `json:"errors,omitempty"`
}

type WhatsAppConversation struct {
	ID     string         `json:"id"`
	Origin WhatsAppOrigin `json:"origin"`
}

type WhatsAppOrigin struct {
	Type string `json:"type"`
}

type WhatsAppPricing struct {
	Billable     bool   `json:"billable"`
	PricingModel string `json:"pricing_model"`
	Category     string `json:"category"`
}

type WhatsAppError struct {
	Code      int            `json:"code"`
	Title     string         `json:"title"`
	Message   string         `json:"message"`
	ErrorData map[string]any `json:"error_data"`
}
