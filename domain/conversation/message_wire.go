package conversation

import "encoding/json"

func (m Message) MarshalJSON() ([]byte, error) {
	type plainMessage Message
	view := struct {
		plainMessage
		Location         *MessageLocation    `json:"location,omitempty"`
		Reactions        []Reaction          `json:"reactions,omitempty"`
		SenderUserID     string              `json:"sender_user_id,omitempty"`
		ResentAs         string              `json:"resent_as,omitempty"`
		Interactive      *InteractiveContent `json:"interactive,omitempty"`
		InteractiveReply *InteractiveReply   `json:"interactive_reply,omitempty"`
	}{plainMessage: plainMessage(m), Reactions: m.Reactions(), SenderUserID: m.SenderUserID(), ResentAs: m.ResentAs()}
	if m.IsRetracted() {
		view.plainMessage = plainMessage(m.withoutContent())
		return json.Marshal(view)
	}
	view.Location = m.wireLocation()
	view.Interactive, view.InteractiveReply = m.wireInteractive()
	return json.Marshal(view)
}

func (m *Message) wireInteractive() (*InteractiveContent, *InteractiveReply) {
	if content, ok := m.Interactive(); ok {
		return &content, nil
	}
	if reply, ok := m.InteractiveReply(); ok {
		return nil, &reply
	}
	return nil, nil
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
