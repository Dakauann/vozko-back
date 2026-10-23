package emailcampaign_usecase

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"vozko/domain/emailaddress"
	"vozko/domain/emailcampaign"
	"vozko/domain/emaildesign"
	"vozko/domain/emailsending"
	"vozko/domain/lead"
	balance_usecase "vozko/usecases/balance"
)

const MaxTestRecipients = 5

type TestResult struct {
	Address   string `json:"address"`
	MessageID string `json:"message_id,omitempty"`
	Outcome   string `json:"outcome"`
}

const (
	TestSent       = "sent"
	TestSuppressed = "suppressed"
	TestDailyCap   = "daily_cap"
	TestFailed     = "failed"
)

type testCharge struct {
	key   string
	quote balance_usecase.EmailQuote
}

func testChargeKey(campaignID string) string {
	return "test:" + campaignID + ":" + uuid.NewString()
}

func (s *Service) TestSend(ctx context.Context, a Actor, campaignID string, to []string) ([]TestResult, error) {
	addresses, err := testAddresses(to)
	if err != nil {
		return nil, err
	}
	c, err := s.deps.Campaigns.Get(ctx, a.WorkspaceID, campaignID)
	if err != nil {
		return nil, err
	}
	if issues := emaildesign.Validate(c.RenderDocument()); len(issues) > 0 {
		return nil, &emaildesign.ValidationError{Kind: emaildesign.ErrInvalidDocument, Issues: issues}
	}
	if issues := emaildesign.ValidateSubject(c.Subject); len(issues) > 0 {
		return nil, &emaildesign.ValidationError{Kind: emaildesign.ErrInvalidSubject, Issues: issues}
	}
	charges, err := s.chargeTests(ctx, *c, len(addresses))
	if err != nil {
		return nil, err
	}
	results := make([]TestResult, 0, len(addresses))
	for i, address := range addresses {
		result, err := s.testOne(ctx, *c, address)
		if err != nil {
			s.refundTests(ctx, c.WorkspaceID, charges[i:])
			return nil, err
		}
		if result.Outcome != TestSent {
			s.refundTests(ctx, c.WorkspaceID, charges[i:i+1])
		}
		results = append(results, result)
	}
	return results, nil
}

func testAddresses(to []string) ([]emailaddress.Address, error) {
	if len(to) == 0 || len(to) > MaxTestRecipients {
		return nil, ErrTestRecipients
	}
	addresses := make([]emailaddress.Address, 0, len(to))
	seen := map[emailaddress.Address]bool{}
	for _, raw := range to {
		address, err := emailaddress.Normalize(raw)
		if err != nil {
			return nil, ErrTestAddressInvalid
		}
		if !seen[address] {
			seen[address] = true
			addresses = append(addresses, address)
		}
	}
	return addresses, nil
}

func (s *Service) chargeTests(ctx context.Context, c emailcampaign.Campaign, recipients int) ([]testCharge, error) {
	charges := make([]testCharge, 0, recipients)
	for range recipients {
		key := testChargeKey(c.ID)
		quote, err := s.deps.Billing.ChargeMessage(ctx, c.WorkspaceID, key)
		if err != nil {
			s.refundTests(ctx, c.WorkspaceID, charges)
			return nil, s.testShortfall(ctx, c.WorkspaceID, err, recipients-len(charges)-1)
		}
		charges = append(charges, testCharge{key: key, quote: quote})
	}
	return charges, nil
}

func (s *Service) testShortfall(ctx context.Context, workspaceID string, err error, uncharged int) error {
	var short *balance_usecase.InsufficientEmailBalanceError
	if !errors.As(err, &short) || uncharged <= 0 {
		return err
	}
	rest, quoteErr := s.deps.Billing.QuoteRun(ctx, workspaceID, int64(uncharged))
	if quoteErr != nil {
		return err
	}
	return &balance_usecase.InsufficientEmailBalanceError{MissingMicros: short.MissingMicros + rest.TotalMicros}
}

func (s *Service) refundTests(ctx context.Context, workspaceID string, charges []testCharge) {
	for _, charge := range charges {
		if err := s.deps.Billing.RefundMessage(ctx, workspaceID, charge.key, charge.quote); err != nil {
			s.logf("CRITICAL test send charge %s of workspace %s was not refunded: %v", charge.key, workspaceID, err)
		}
	}
}

func (s *Service) testOne(ctx context.Context, c emailcampaign.Campaign, address emailaddress.Address) (TestResult, error) {
	subject, p := s.testMerge(c, address)
	sent, err := s.deps.Gateway.Send(ctx, emailsending.SendRequest{
		WorkspaceID: c.WorkspaceID,
		SenderID:    c.SenderID,
		To:          address,
		Kind:        emailsending.KindMarketing,
		RunKind:     emailsending.RunTest,
		RunID:       c.ID,
		Tags:        map[string]string{"campaign_id": c.ID, "test": "true"},
	}, s.compose(c, subject, p))
	var capErr *emailsending.DailyCapError
	switch {
	case err == nil:
		result := TestResult{Address: address.String(), Outcome: TestSent}
		if sent != nil {
			result.MessageID = sent.ID
		}
		return result, nil
	case errors.Is(err, emailsending.ErrRecipientSuppressed):
		return TestResult{Address: address.String(), Outcome: TestSuppressed}, nil
	case errors.As(err, &capErr):
		return TestResult{Address: address.String(), Outcome: TestDailyCap}, nil
	}
	if kind, ok := emailsending.SendErrorKindOf(err); ok && kind == emailsending.SendErrorRejected {
		return TestResult{Address: address.String(), Outcome: TestFailed}, nil
	}
	return TestResult{}, err
}

func (s *Service) testMerge(c emailcampaign.Campaign, address emailaddress.Address) (string, emaildesign.Personalization) {
	return c.Subject, c.Personalize(s.testPersonalization(c.WorkspaceID, address))
}

func (s *Service) testPersonalization(workspaceID string, address emailaddress.Address) emaildesign.Personalization {
	found, err := s.deps.Leads.FindByEmail(workspaceID, address.String())
	if err != nil || found == nil {
		return emaildesign.Personalization{Email: address.String()}
	}
	return personalization(lead.EmailFacts{Name: found.Name, CustomFields: found.CustomFields}, address)
}
