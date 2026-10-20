package conversation

import "encoding/json"

func (m Message) MarshalJSON() ([]byte, error) {
	type plainMessage Message
	view := struct {
		plainMessage
		Location     *MessageLocation `json:"location,omitempty"`
		Reactions    []Reaction       `json:"reactions,omitempty"`
		SenderUserID string           `json:"sender_user_id,omitempty"`
		ResentAs     string           `json:"resent_as,omitempty"`
	}{plainMessage: plainMessage(m), Reactions: m.Reactions(), SenderUserID: m.SenderUserID(), ResentAs: m.ResentAs()}
	if m.IsRetracted() {
		view.plainMessage = plainMessage(m.withoutContent())
		return json.Marshal(view)
	}
	view.Location = m.wireLocation()
	return json.Marshal(view)
}

func (m Message) withoutContent() Message {
	m.Text = ""
	m.Transcript = ""
	m.Image = nil
	m.Video = nil
	m.MediaID = nil
	m.MediaType = ""
	m.Media = nil
	m.Metadata = nil
	return m
}

func (m *Message) wireLocation() *MessageLocation {
	found, ok := LocationOf(m)
	if !ok {
		return nil
	}
	_, candidate := LocationCandidateOf(m)
	return &MessageLocation{
		Latitude: found.Point.Lat, Longitude: found.Point.Lng,
		Name: found.Name, Address: found.Address, Candidate: candidate,
	}
}
