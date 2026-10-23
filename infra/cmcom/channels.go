package cmcom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"vozko/domain/carriermessaging"
	"vozko/domain/rcs"
)

const (
	rcsChannelTypeName  = "RCS"
	profileCategoryName = "Profile"
	formStateComplete   = "COMPLETE"
	formStateIncomplete = "INCOMPLETE"
	maxFormRounds       = 10
	channelTypesPage    = "100"
)

var ErrFormIncomplete = errors.New("cmcom: the provider form did not complete")

type channelIDs struct {
	typeID     string
	categoryID string
}

type wireChannelType struct {
	ID         string `json:"id"`
	PrettyName string `json:"prettyName"`
}

type wireCategory struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type wireIdentified struct {
	ID string `json:"id"`
}

type wireRequestForm struct {
	FormElements     []formElement `json:"formElements"`
	DynamicFormState string        `json:"dynamicFormState"`
}

type wireProfile struct {
	ID              string `json:"id"`
	EntityStatus    string `json:"entityStatus"`
	RejectionReason string `json:"rejectionReason"`
}

var providerStates = map[string]rcs.ProviderState{
	"ACTIVE":                   rcs.ProviderLaunched,
	"PENDING":                  rcs.ProviderPending,
	"ELIGIBLE":                 rcs.ProviderPending,
	"NOT_VALIDATED":            rcs.ProviderAwaitingBrand,
	"CUSTOMER_ACTION_REQUIRED": rcs.ProviderAwaitingBrand,
	"REJECTED":                 rcs.ProviderRejected,
	"EXPIRED":                  rcs.ProviderRejected,
	"CANCELLED":                rcs.ProviderRejected,
	"DELETED":                  rcs.ProviderRejected,
}

func (c *Client) accountURL(segments ...string) string {
	escaped := make([]string, len(segments))
	for i, s := range segments {
		escaped[i] = url.PathEscape(s)
	}
	return c.apiURL + "/channels/v1/accounts/" + url.PathEscape(c.accountID) + "/" + strings.Join(escaped, "/")
}

func withQuery(base string, query url.Values) string {
	return base + "?" + query.Encode()
}

func (c *Client) call(ctx context.Context, method, target, operation string, payload, out any) error {
	res, err := c.do(ctx, method, target, payload, carriermessaging.ErrProviderUnavailable)
	if err != nil {
		return err
	}
	if res.status < http.StatusOK || res.status >= http.StatusMultipleChoices {
		return statusError(res, operation, carriermessaging.ErrProviderUnavailable)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(res.body, out); err != nil {
		return fmt.Errorf("%w: %s answered %q", ErrProviderResponse, operation, res.body)
	}
	return nil
}

func (c *Client) channelIDs(ctx context.Context) (channelIDs, error) {
	c.discoveryMu.Lock()
	defer c.discoveryMu.Unlock()
	if c.discovered != nil {
		return *c.discovered, nil
	}
	var types []wireChannelType
	if err := c.call(ctx, http.MethodGet, withQuery(c.accountURL("channels", "types"), url.Values{"take": {channelTypesPage}}), "list channel types", nil, &types); err != nil {
		return channelIDs{}, err
	}
	typeID, err := onlyMatch(types, func(t wireChannelType) (string, bool) { return t.ID, namesRCS(t.PrettyName) }, "RCS channel type")
	if err != nil {
		return channelIDs{}, err
	}
	var categories []wireCategory
	if err := c.call(ctx, http.MethodGet, c.accountURL("channels", "types", typeID, "categories"), "list channel categories", nil, &categories); err != nil {
		return channelIDs{}, err
	}
	categoryID, err := onlyMatch(categories, func(cat wireCategory) (string, bool) {
		return cat.ID, strings.EqualFold(strings.TrimSpace(cat.Name), profileCategoryName)
	}, "RCS profile category")
	if err != nil {
		return channelIDs{}, err
	}
	c.discovered = &channelIDs{typeID: typeID, categoryID: categoryID}
	return *c.discovered, nil
}

func namesRCS(prettyName string) bool {
	for _, word := range strings.FieldsFunc(prettyName, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9')
	}) {
		if strings.EqualFold(word, rcsChannelTypeName) {
			return true
		}
	}
	return false
}

func onlyMatch[T any](items []T, match func(T) (string, bool), what string) (string, error) {
	var found []string
	for _, item := range items {
		if id, ok := match(item); ok {
			found = append(found, id)
		}
	}
	if len(found) != 1 {
		return "", fmt.Errorf("%w: expected one %s, found %d", ErrProviderResponse, what, len(found))
	}
	if !guidPattern.MatchString(found[0]) {
		return "", fmt.Errorf("%w: the %s id %q is not a guid", ErrProviderResponse, what, found[0])
	}
	return found[0], nil
}

func senderIDs(s rcs.Sender) (string, string, error) {
	requestID := strings.TrimSpace(s.ProviderRequestID)
	profileID := strings.TrimSpace(s.ProviderProfileID)
	if requestID != "" && !guidPattern.MatchString(requestID) {
		return "", "", fmt.Errorf("%w: request id %q", ErrSenderIDs, requestID)
	}
	if profileID != "" && !guidPattern.MatchString(profileID) {
		return "", "", fmt.Errorf("%w: profile id %q", ErrSenderIDs, profileID)
	}
	if profileID != "" && requestID == "" {
		return "", "", fmt.Errorf("%w: a profile id needs its channel request id", ErrSenderIDs)
	}
	return requestID, profileID, nil
}

func (c *Client) SubmitSender(ctx context.Context, s rcs.Sender) (carriermessaging.SenderSubmission, error) {
	requestID, profileID, err := senderIDs(s)
	if err != nil {
		return carriermessaging.SenderSubmission{}, err
	}
	ids, err := c.channelIDs(ctx)
	if err != nil {
		return carriermessaging.SenderSubmission{}, err
	}
	if requestID == "" {
		if requestID, err = c.create(ctx, withQuery(c.accountURL("channels", "requests"), url.Values{"channelTypeId": {ids.typeID}}), "create channel request"); err != nil {
			return carriermessaging.SenderSubmission{}, err
		}
	}
	if profileID == "" {
		if profileID, err = c.create(ctx, c.accountURL("rcs", requestID, "profiles"), "create RCS profile"); err != nil {
			return carriermessaging.SenderSubmission{RequestID: requestID}, err
		}
	}
	submission := carriermessaging.SenderSubmission{RequestID: requestID, ProfileID: profileID}
	return submission, c.completeForm(ctx, ids, submission, s)
}

func (c *Client) create(ctx context.Context, target, operation string) (string, error) {
	var created wireIdentified
	if err := c.call(ctx, http.MethodPost, target, operation, nil, &created); err != nil {
		return "", err
	}
	if !guidPattern.MatchString(created.ID) {
		return "", fmt.Errorf("%w: %s returned id %q", ErrProviderResponse, operation, created.ID)
	}
	return created.ID, nil
}

func (c *Client) completeForm(ctx context.Context, ids channelIDs, sub carriermessaging.SenderSubmission, s rcs.Sender) error {
	formURL := withQuery(c.accountURL("channels", "types", ids.typeID, "requestform"), url.Values{
		"channelTypeCategoryId":    {ids.categoryID},
		"channelCategoryRequestId": {sub.ProfileID},
		"omitTitles":               {"true"},
	})
	inputURL := c.accountURL("rcs", sub.RequestID, "profiles", sub.ProfileID, "forminput")
	submitted := map[string]string{}
	for range maxFormRounds {
		var form wireRequestForm
		if err := c.call(ctx, http.MethodGet, formURL, "get request form", nil, &form); err != nil {
			return err
		}
		switch form.DynamicFormState {
		case formStateComplete:
			return nil
		case formStateIncomplete:
		default:
			return fmt.Errorf("%w: request form state %q", ErrProviderResponse, form.DynamicFormState)
		}
		inputs, err := formInputs(form.FormElements, s)
		if err != nil {
			return err
		}
		var pending []formInput
		for _, in := range inputs {
			if previous, sent := submitted[in.FormElementID]; !sent || previous != in.InputValue {
				pending = append(pending, in)
			}
		}
		if len(pending) == 0 {
			return fmt.Errorf("%w: still waiting on %s", ErrFormIncomplete, strings.Join(requiredKeys(form.FormElements), ", "))
		}
		if err := c.call(ctx, http.MethodPut, inputURL, "add form input", pending, nil); err != nil {
			return err
		}
		for _, in := range pending {
			submitted[in.FormElementID] = in.InputValue
		}
	}
	return fmt.Errorf("%w: gave up after %d saves", ErrFormIncomplete, maxFormRounds)
}

func (c *Client) SenderStatus(ctx context.Context, s rcs.Sender) (carriermessaging.SenderStatusReport, error) {
	requestID, profileID, err := senderIDs(s)
	if err != nil {
		return carriermessaging.SenderStatusReport{}, err
	}
	if requestID == "" || profileID == "" {
		return carriermessaging.SenderStatusReport{}, fmt.Errorf("%w: the sender was never submitted", ErrSenderIDs)
	}
	var profile wireProfile
	if err := c.call(ctx, http.MethodGet, c.accountURL("rcs", requestID, "profiles", profileID), "get RCS profile", nil, &profile); err != nil {
		return carriermessaging.SenderStatusReport{}, err
	}
	state, ok := providerStates[profile.EntityStatus]
	if !ok {
		return carriermessaging.SenderStatusReport{}, fmt.Errorf("%w: profile status %q", ErrProviderResponse, profile.EntityStatus)
	}
	report := carriermessaging.SenderStatusReport{State: state}
	if state == rcs.ProviderRejected {
		report.Reason = strings.TrimSpace(profile.RejectionReason)
	}
	return report, nil
}
