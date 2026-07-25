package dialerringchannels

type SetRingChannelsRequest struct {
	Channels []string `json:"channels" example:"browser,branch"`
}

type RingChannelsResponse struct {
	Channels []string `json:"channels" example:"browser,branch"`
}
