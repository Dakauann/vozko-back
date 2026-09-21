package businessphone

type Dialog360Channel struct {
	ID                 string
	WABAExternalID     string
	PhoneNumber        string
	PhoneName          string
	QualityRating      string
	MessagingTier      string
	ReviewStatus       string
	WABAName           string
	Status             string
	HubStatus          string
	Cancelled          bool
	AvailabilityStatus string
	IsOnBizApp         bool
}

func (c Dialog360Channel) IsDeactivated() bool {
	if c.Cancelled {
		return true
	}
	switch c.HubStatus {
	case "pending_deletion", "deleted", "terminated", "cancelled":
		return true
	}
	switch c.ReviewStatus {
	case "bsp_removed", "disabled", "banned":
		return true
	}
	return false
}

type Dialog360Balance struct {
	Amount   float64
	Currency string
}

type RegisterNumberInput struct {
	ClientID          string
	WABAExternalID    string
	ChannelExternalID string
}

type APIKeyResult struct {
	APIKey  string
	Address string
}

type Dialog360PartnerService interface {
	CreateClient(name, email string) (clientID string, err error)
	FindClientByEmail(email string) (clientID string, err error)
	RegisterNumber(input RegisterNumberInput) error
	ListChannels() ([]Dialog360Channel, error)
	GetChannel(channelID string) (*Dialog360Channel, error)
	GenerateAPIKey(channelID string) (*APIKeyResult, error)
	GetPartnerBalance() (*Dialog360Balance, error)
	CancelChannel(clientID, channelID string) error
	ReactivateChannel(clientID, channelID string) error
	SetWebhookURL(webhookURL string) error
}
