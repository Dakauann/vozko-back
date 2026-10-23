package emailinbox

import (
	"encoding/json"
	"errors"
	"strings"

	"vozko/domain/emaildesign"
)

const (
	MetadataHTMLMediaID   = "email_html_media_id"
	MetadataSentMessageID = "email_sent_message_id"
	MetadataCampaignMerge = "email_campaign_merge"
	HTMLMimeType          = "text/html"
	MaxStoredHTMLBytes    = 2 << 20
)

var (
	ErrHTMLUnsafe = errors.New("emailinbox: the html could not be made safe")
	ErrNotAnEmail = errors.New("emailinbox: the message is not an email of this conversation")
)

type CampaignMerge struct {
	Subject         string                      `json:"subject"`
	Personalization emaildesign.Personalization `json:"personalization"`
}

type HTMLRef struct {
	MediaID       string
	CampaignID    string
	SentMessageID string
	Merge         *CampaignMerge
}

func (r HTMLRef) Stored() bool { return r.MediaID != "" }

func (r HTMLRef) CampaignOriginal() bool {
	return r.CampaignID != "" && r.SentMessageID != "" && r.Merge != nil
}

func HTMLRefOf(metadata json.RawMessage) HTMLRef {
	var raw struct {
		MediaID       string         `json:"email_html_media_id"`
		CampaignID    string         `json:"email_campaign_id"`
		SentMessageID string         `json:"email_sent_message_id"`
		Merge         *CampaignMerge `json:"email_campaign_merge"`
	}
	if len(metadata) == 0 || json.Unmarshal(metadata, &raw) != nil {
		return HTMLRef{}
	}
	return HTMLRef{
		MediaID: strings.TrimSpace(raw.MediaID), CampaignID: strings.TrimSpace(raw.CampaignID),
		SentMessageID: strings.TrimSpace(raw.SentMessageID), Merge: raw.Merge,
	}
}

type SanitizedHTML struct {
	HTML                string
	RemoteImagesBlocked bool
}

type HTMLSanitizer interface {
	Sanitize(html string, allowRemoteImages bool) (SanitizedHTML, error)
}

type HTMLView struct {
	HTML          string `json:"html"`
	Text          string `json:"text"`
	HasHTML       bool   `json:"has_html"`
	Inbound       bool   `json:"inbound"`
	ImagesBlocked bool   `json:"images_blocked"`
}
