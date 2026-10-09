package conversation

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"vozko/domain/geo"
)

const (
	locationMetadataKey  = `"location"`
	telegramLatitudeKey  = `"telegram_latitude"`
	locationTextPin      = "📍 "
	locationTextFormat   = locationTextPin + "%s, %s (%.6f, %.6f)"
	locationLabelCutset  = ", "
	locationTextGroups   = 4
	locationTextLatGroup = 2
	locationTextLngGroup = 3
)

var locationTextPattern = regexp.MustCompile(`(?s)^📍 (.*) \((-?\d{1,3}(?:\.\d+)?), (-?\d{1,3}(?:\.\d+)?)\)$`)

type Location struct {
	Point   geo.Point
	Name    string
	Address string
}

type LocationCandidate struct {
	MessageID string
	Point     geo.Point
	Channel   MessageChannel
}

type MessageLocation struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Name      string  `json:"name,omitempty"`
	Address   string  `json:"address,omitempty"`
	Candidate bool    `json:"candidate"`
}

type storedCoordinates struct {
	Latitude  *float64 `json:"latitude"`
	Longitude *float64 `json:"longitude"`
	Name      string   `json:"name,omitempty"`
	Address   string   `json:"address,omitempty"`
}

type storedLocation struct {
	Location          *storedCoordinates `json:"location"`
	TelegramLatitude  *float64           `json:"telegram_latitude"`
	TelegramLongitude *float64           `json:"telegram_longitude"`
}

func LocationText(name, address string, p geo.Point) string {
	return fmt.Sprintf(locationTextFormat, name, address, p.Lat, p.Lng)
}

func (l *WhatsAppLocation) Text() string {
	if l == nil {
		return ""
	}
	return LocationText(l.Name, l.Address, geo.Point{Lat: l.Latitude, Lng: l.Longitude})
}

func (l *WhatsAppLocation) Metadata() json.RawMessage {
	if l == nil {
		return nil
	}
	lat, lng := l.Latitude, l.Longitude
	raw, err := json.Marshal(struct {
		Location storedCoordinates `json:"location"`
	}{Location: storedCoordinates{Latitude: &lat, Longitude: &lng, Name: l.Name, Address: l.Address}})
	if err != nil {
		return nil
	}
	return raw
}

func LocationOf(m *Message) (Location, bool) {
	if m == nil {
		return Location{}, false
	}
	found, ok := locationInMetadata(m.Channel, m.Metadata)
	if !ok && m.Channel == MessageChannelWhatsApp {
		found, ok = locationInText(m.Text)
	}
	if !ok || found.Point.Validate() != nil {
		return Location{}, false
	}
	return found, true
}

func LocationCandidateOf(m *Message) (LocationCandidate, bool) {
	found, ok := LocationOf(m)
	if !ok || strings.TrimSpace(m.ID) == "" || m.ResolvedDirection() != MessageDirectionInbound || !found.Point.InBrazil() {
		return LocationCandidate{}, false
	}
	return LocationCandidate{MessageID: m.ID, Point: found.Point, Channel: m.Channel}, true
}

func locationInMetadata(channel MessageChannel, raw json.RawMessage) (Location, bool) {
	if !bytes.Contains(raw, []byte(locationMetadataKey)) && !bytes.Contains(raw, []byte(telegramLatitudeKey)) {
		return Location{}, false
	}
	var stored storedLocation
	if err := json.Unmarshal(raw, &stored); err != nil {
		return Location{}, false
	}
	if c := stored.Location; c != nil && c.Latitude != nil && c.Longitude != nil {
		return Location{Point: geo.Point{Lat: *c.Latitude, Lng: *c.Longitude}, Name: strings.TrimSpace(c.Name), Address: strings.TrimSpace(c.Address)}, true
	}
	if channel == MessageChannelTelegram && stored.TelegramLatitude != nil && stored.TelegramLongitude != nil {
		return Location{Point: geo.Point{Lat: *stored.TelegramLatitude, Lng: *stored.TelegramLongitude}}, true
	}
	return Location{}, false
}

func locationInText(text string) (Location, bool) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, locationTextPin) {
		return Location{}, false
	}
	match := locationTextPattern.FindStringSubmatch(text)
	if len(match) != locationTextGroups {
		return Location{}, false
	}
	lat, latErr := strconv.ParseFloat(match[locationTextLatGroup], 64)
	lng, lngErr := strconv.ParseFloat(match[locationTextLngGroup], 64)
	if latErr != nil || lngErr != nil {
		return Location{}, false
	}
	return Location{Point: geo.Point{Lat: lat, Lng: lng}, Address: strings.Trim(match[1], locationLabelCutset)}, true
}

func (m Message) MarshalJSON() ([]byte, error) {
	type plainMessage Message
	view := struct {
		plainMessage
		Location *MessageLocation `json:"location,omitempty"`
	}{plainMessage: plainMessage(m)}
	if found, ok := LocationOf(&m); ok {
		_, candidate := LocationCandidateOf(&m)
		view.Location = &MessageLocation{
			Latitude: found.Point.Lat, Longitude: found.Point.Lng,
			Name: found.Name, Address: found.Address, Candidate: candidate,
		}
	}
	return json.Marshal(view)
}
