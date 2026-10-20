package tiktok

import "time"

const sideMessageTTL = time.Hour

type SideMessageState interface {
	SetString(key, value string, ttl time.Duration) error
	Exists(key string) (bool, error)
}

type SideMessages struct {
	state SideMessageState
}

func NewSideMessages(state SideMessageState) *SideMessages {
	if state == nil {
		panic("tiktok: side messages require the shared state")
	}
	return &SideMessages{state: state}
}

func sideMessageKey(messageID string) string { return "tiktok:side_message:" + messageID }

func (s *SideMessages) Mark(messageID string) error {
	return s.state.SetString(sideMessageKey(messageID), "1", sideMessageTTL)
}

func (s *SideMessages) Seen(messageID string) bool {
	seen, err := s.state.Exists(sideMessageKey(messageID))
	return err == nil && seen
}
