package carriermessaging_usecase

import (
	"context"

	"vozko/domain/rcs"
)

func (s *Service) prepareRCS(ctx context.Context, req DeliverRequest) (outbound, error) {
	sender, err := s.Repos.Senders.FindByID(ctx, req.WorkspaceID, req.SenderID)
	if err != nil {
		return outbound{}, err
	}
	if !sender.CanSend() {
		return outbound{}, rcs.ErrSenderCannotSend
	}
	content := req.Content.Render(req.Vars)
	if err := content.Validate(); err != nil {
		return outbound{}, err
	}
	return outbound{
		senderID: sender.ID, from: sender.ProviderProfileID, content: content, class: content.BillingClass(),
		conversational: sender.BillingCategory == rcs.BillingConversational,
	}, nil
}

func (s *Service) tracked(content rcs.Content, messageID string) (rcs.Content, error) {
	out := content.WithTrackedPostbacks()
	for _, b := range out.Buttons() {
		if b.Suggestion.Kind != rcs.SuggestOpenURL {
			continue
		}
		url, err := s.Links.TrackedURL(messageID, b.Key)
		if err != nil {
			return rcs.Content{}, err
		}
		setButtonURL(&out, b.Key, url)
	}
	return out, nil
}

func setButtonURL(c *rcs.Content, key rcs.ButtonKey, url string) {
	if key.Card < 0 {
		c.Suggestions[key.Index].URL = url
		return
	}
	c.Cards[key.Card].Suggestions[key.Index].URL = url
}
