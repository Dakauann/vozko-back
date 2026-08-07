package uazapi

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	uw "vozko/domain/unofficial_whatsapp"
)

// The chat-profile read.
//
// One endpoint, `POST /chat/details`, answers "who is this" for BOTH a person
// and a group: it accepts a phone number or a group JID and returns the vendor's
// Chat object either way. That is why domain/unofficial_whatsapp has a single
// ChatProfile rather than a contact profile and a group profile — the split
// would have been ours to invent, and it is exactly where the name half and the
// picture half of an identity drift apart.

// chatDetailsResponse is the vendor's Chat object, narrowed to the fields that
// answer "who is this".
//
// The full object carries more than sixty fields, most of them the vendor's own
// lead/CRM store (lead_name, lead_status, lead_field01..20) and its built-in
// chatbot's state. None of that is decoded: those are a competing CRM's records,
// and reading them here would let another system's data leak into ours.
type chatDetailsResponse struct {
	WAChatID  string `json:"wa_chatid"`
	WAChatLID string `json:"wa_chatlid"`
	Phone     string `json:"phone"`

	// Three names with a real precedence, not synonyms:
	//   wa_contactName — what the CONNECTED PHONE has them saved as. Exists only
	//                    when the number is in the owner's address book, and it
	//                    outranks everything because it is what the operator
	//                    already calls this person.
	//   wa_name        — the WhatsApp profile name, chosen by the contact.
	//   name           — the vendor's own consolidated fallback.
	// A group puts its subject in `name`.
	WAContactName string `json:"wa_contactName"`
	WAName        string `json:"wa_name"`
	Name          string `json:"name"`

	// Both sizes are returned depending on the `preview` flag, and which one
	// arrives is not worth branching on: whichever is present is the avatar.
	Image        string `json:"image"`
	ImagePreview string `json:"imagePreview"`

	WAIsGroup   bool `json:"wa_isGroup"`
	WAIsBlocked bool `json:"wa_isBlocked"`
	// The vendor exposes no is-business flag on Chat; a verified business name
	// arrives through the message stream instead. Left absent rather than
	// guessed from the presence of a name.
}

// ChatDetails reads one chat subject's profile.
//
// `preview: true` asks for the thumbnail rather than the full-resolution
// original. The bytes are re-hosted by the caller and rendered in a 40px circle,
// so fetching the original would move several hundred kilobytes per contact to
// produce an identical result — and this call competes with sends for the same
// instance's budget.
func (c *Client) ChatDetails(ctx context.Context, ref uw.InstanceRef, chatID string) (*uw.ChatProfile, error) {
	chatID = strings.TrimSpace(chatID)
	if chatID == "" {
		return nil, uw.ErrContactNotFound
	}

	var resp chatDetailsResponse
	err := c.instanceCall(ctx, ref, http.MethodPost, "/chat/details", map[string]any{
		"number":  chatID,
		"preview": true,
	}, &resp)
	if err != nil {
		return nil, err
	}

	jid := firstNonEmpty(resp.WAChatID, chatID)
	profile := &uw.ChatProfile{
		JID:         jid,
		LID:         resp.WAChatLID,
		Name:        firstNonEmpty(resp.WAName, resp.Name),
		ContactName: resp.WAContactName,
		// imagePreview is what `preview: true` fills; `image` is read as a
		// fallback so a host that ignores the flag still yields an avatar
		// instead of silently returning none.
		PictureURL: firstNonEmpty(resp.ImagePreview, resp.Image),
		IsGroup:    resp.WAIsGroup || uw.IsGroupJID(jid),
		IsBlocked:  resp.WAIsBlocked,
	}
	if !profile.IsGroup {
		profile.PhoneNumber = uw.NormalizePhone(resp.Phone)
		if profile.PhoneNumber == "" {
			profile.PhoneNumber = uw.PhoneFromJID(jid)
		}
	}
	return profile, nil
}

// FetchAsset downloads a provider-hosted URL — today, a profile picture.
//
// Deliberately not routed through call(): the URL points at WhatsApp's own CDN
// rather than at the host, so it takes no instance token (sending one would leak
// the credential to a third party) and returns bytes rather than JSON.
//
// The size cap is enforced with one byte of headroom so an oversized asset is
// DETECTED rather than silently truncated into a corrupt image that renders as a
// broken avatar forever.
func (c *Client) FetchAsset(ctx context.Context, url string) ([]byte, string, error) {
	url = strings.TrimSpace(url)
	if url == "" {
		return nil, "", fmt.Errorf("uazapi: no asset url")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", fmt.Errorf("uazapi: bad asset url: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("uazapi: fetch asset: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("uazapi: asset fetch returned %d", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, uw.MaxAvatarBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("uazapi: read asset: %w", err)
	}
	if len(data) > uw.MaxAvatarBytes {
		return nil, "", fmt.Errorf("uazapi: asset exceeds the %d byte limit", uw.MaxAvatarBytes)
	}
	return data, resp.Header.Get("Content-Type"), nil
}
