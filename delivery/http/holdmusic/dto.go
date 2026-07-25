package holdmusic

type HoldTrackResponse struct {
	Key   string `json:"key" example:"bossa_nova"`
	Label string `json:"label" example:"Bossa Nova"`
}

type HoldTrackListResponse struct {
	Tracks []HoldTrackResponse `json:"tracks"`
}
