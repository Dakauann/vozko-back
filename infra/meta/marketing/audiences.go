package marketing

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"vozko/domain/advertising"
	"vozko/infra/meta"
)

var _ advertising.AudienceGateway = (*Gateway)(nil)

const (
	customAudienceTermsKey   = "custom_audience_tos"
	customerFileSource       = "USER_PROVIDED_ONLY"
	unavailableAudienceCount = -1
)

const audienceFields = "id,name,description,subtype,approximate_count_lower_bound,approximate_count_upper_bound,delivery_status,operation_status,lookalike_spec,retention_days,time_created,time_updated"

var audienceKinds = map[string]advertising.AudienceKind{
	"CUSTOM":     advertising.AudienceCustomerList,
	"LOOKALIKE":  advertising.AudienceLookalike,
	"WEBSITE":    advertising.AudienceWebsite,
	"ENGAGEMENT": advertising.AudienceEngagement,
}

func (g *Gateway) CustomAudienceTermsAccepted(ctx context.Context, token, metaAccountID string) (bool, error) {
	path, err := accountPath(metaAccountID)
	if err != nil {
		return false, err
	}
	var out struct {
		TermsAccepted map[string]graphNumber `json:"tos_accepted"`
	}
	if err := g.do(ctx, meta.Request{Method: http.MethodGet, Path: path, Token: token, Query: url.Values{"fields": {"tos_accepted"}}}, &out); err != nil {
		return false, err
	}
	return out.TermsAccepted[customAudienceTermsKey] == "1", nil
}

type graphAudienceStatus struct {
	Code        int    `json:"code"`
	Description string `json:"description"`
}

type graphAudience struct {
	ID            meta.GraphID        `json:"id"`
	Name          string              `json:"name"`
	Description   string              `json:"description"`
	Subtype       string              `json:"subtype"`
	LowerBound    graphNumber         `json:"approximate_count_lower_bound"`
	UpperBound    graphNumber         `json:"approximate_count_upper_bound"`
	Delivery      graphAudienceStatus `json:"delivery_status"`
	Operation     graphAudienceStatus `json:"operation_status"`
	RetentionDays graphNumber         `json:"retention_days"`
	TimeCreated   json.RawMessage     `json:"time_created"`
	TimeUpdated   json.RawMessage     `json:"time_updated"`
	Lookalike     *struct {
		Ratio   graphNumber `json:"ratio"`
		Country string      `json:"country"`
		Origin  []struct {
			ID meta.GraphID `json:"id"`
		} `json:"origin"`
	} `json:"lookalike_spec"`
}

func (g *Gateway) ListAudiences(ctx context.Context, token, metaAccountID string) ([]advertising.Audience, error) {
	path, err := accountPath(metaAccountID)
	if err != nil {
		return nil, err
	}
	rows, err := collect[graphAudience](ctx, g, path+"/customaudiences", token, url.Values{"fields": {audienceFields}, "limit": {"100"}})
	if err != nil {
		return nil, err
	}
	audiences := make([]advertising.Audience, 0, len(rows))
	for _, row := range rows {
		audience, err := row.toDomain()
		if err != nil {
			return nil, err
		}
		audiences = append(audiences, audience)
	}
	return audiences, nil
}

func (r graphAudience) toDomain() (advertising.Audience, error) {
	if r.ID == "" {
		return advertising.Audience{}, fmt.Errorf("marketing: custom audience without an id")
	}
	a := advertising.Audience{
		MetaID:               r.ID.String(),
		Name:                 r.Name,
		Description:          r.Description,
		Kind:                 audienceKindOf(r.Subtype),
		DeliveryCode:         r.Delivery.Code,
		DeliveryDescription:  r.Delivery.Description,
		OperationCode:        r.Operation.Code,
		OperationDescription: r.Operation.Description,
	}
	var err error
	if a.ApproxLower, err = audienceCount("approximate_count_lower_bound", r.LowerBound); err != nil {
		return advertising.Audience{}, err
	}
	if a.ApproxUpper, err = audienceCount("approximate_count_upper_bound", r.UpperBound); err != nil {
		return advertising.Audience{}, err
	}
	retention, err := r.RetentionDays.wholePart("retention_days")
	if err != nil {
		return advertising.Audience{}, err
	}
	a.RetentionDays = int(retention)
	if a.CreatedTime, err = audienceTime("time_created", r.TimeCreated); err != nil {
		return advertising.Audience{}, err
	}
	if a.UpdatedTime, err = audienceTime("time_updated", r.TimeUpdated); err != nil {
		return advertising.Audience{}, err
	}
	if r.Lookalike != nil {
		a.LookalikeCountry = r.Lookalike.Country
		if len(r.Lookalike.Origin) > 0 {
			a.OriginAudienceID = r.Lookalike.Origin[0].ID.String()
		}
		if r.Lookalike.Ratio != "" {
			ratio, err := strconv.ParseFloat(string(r.Lookalike.Ratio), 64)
			if err != nil {
				return advertising.Audience{}, fmt.Errorf("marketing: lookalike ratio %q is not a number", string(r.Lookalike.Ratio))
			}
			a.LookalikeRatio = ratio
		}
	}
	return a, nil
}

func audienceKindOf(subtype string) advertising.AudienceKind {
	if kind, ok := audienceKinds[strings.ToUpper(strings.TrimSpace(subtype))]; ok {
		return kind
	}
	return advertising.AudienceOther
}

func audienceCount(field string, raw graphNumber) (int64, error) {
	if raw == "" {
		return unavailableAudienceCount, nil
	}
	n, err := strconv.ParseInt(string(raw), 10, 64)
	if err != nil || n < unavailableAudienceCount {
		return 0, fmt.Errorf("marketing: %s %q is not a count", field, string(raw))
	}
	return n, nil
}

func audienceTime(field string, raw json.RawMessage) (*time.Time, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil, nil
	}
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, fmt.Errorf("marketing: %s is not a time: %w", field, err)
		}
		if seconds, err := strconv.ParseInt(s, 10, 64); err == nil {
			return unixTime(seconds), nil
		}
		return graphTime(field, s)
	}
	seconds, err := strconv.ParseInt(trimmed, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("marketing: %s %s is not a unix time", field, trimmed)
	}
	return unixTime(seconds), nil
}

func unixTime(seconds int64) *time.Time {
	t := time.Unix(seconds, 0).UTC()
	return &t
}

func (g *Gateway) CreateCustomerList(ctx context.Context, token, metaAccountID, name, description string) (string, error) {
	path, err := accountPath(metaAccountID)
	if err != nil {
		return "", err
	}
	form := url.Values{
		"name":                 {name},
		"subtype":              {"CUSTOM"},
		"customer_file_source": {customerFileSource},
	}
	if strings.TrimSpace(description) != "" {
		form.Set("description", description)
	}
	return g.created(ctx, meta.Request{Method: http.MethodPost, Path: path + "/customaudiences", Token: token, Form: form}, "customer list")
}

type graphCustomerSession struct {
	SessionID         int64 `json:"session_id"`
	BatchSeq          int   `json:"batch_seq"`
	LastBatchFlag     bool  `json:"last_batch_flag"`
	EstimatedNumTotal int   `json:"estimated_num_total"`
}

type graphCustomerPayload struct {
	Schema []advertising.MatchKey `json:"schema"`
	Data   [][]string             `json:"data"`
}

type graphCustomerUpload struct {
	Session graphCustomerSession `json:"session"`
	Payload graphCustomerPayload `json:"payload"`
}

func (g *Gateway) AddCustomers(ctx context.Context, token, audienceID string, batch advertising.HashedCustomers, session advertising.CustomerSession) error {
	path, err := objectPath(audienceID)
	if err != nil {
		return err
	}
	if len(batch.Keys) == 0 {
		return fmt.Errorf("marketing: customer batch has no schema")
	}
	if len(batch.Rows) == 0 {
		return fmt.Errorf("marketing: customer batch has no rows")
	}
	if len(batch.Rows) > advertising.CustomerBatchSize() {
		return fmt.Errorf("marketing: customer batch has %d rows, the limit is %d", len(batch.Rows), advertising.CustomerBatchSize())
	}
	for i, row := range batch.Rows {
		if len(row) != len(batch.Keys) {
			return fmt.Errorf("marketing: customer row %d has %d cells for %d keys", i, len(row), len(batch.Keys))
		}
	}
	body := graphCustomerUpload{
		Session: graphCustomerSession{
			SessionID:         session.ID,
			BatchSeq:          session.BatchSeq,
			LastBatchFlag:     session.LastBatch,
			EstimatedNumTotal: session.TotalRows,
		},
		Payload: graphCustomerPayload{Schema: batch.Keys, Data: batch.Rows},
	}
	var out struct {
		NumReceived *graphNumber `json:"num_received"`
	}
	if err := g.do(ctx, meta.Request{Method: http.MethodPost, Path: path + "/users", Token: token, Body: body}, &out); err != nil {
		return err
	}
	if out.NumReceived == nil {
		return fmt.Errorf("marketing: customer upload to %s was not acknowledged", audienceID)
	}
	received, err := out.NumReceived.wholePart("num_received")
	if err != nil {
		return err
	}
	if expected := int64(session.SentRows + len(batch.Rows)); received != expected {
		return fmt.Errorf("marketing: meta received %d of %d customer rows", received, expected)
	}
	return nil
}

type graphLookalikeSpec struct {
	Ratio   float64 `json:"ratio"`
	Country string  `json:"country"`
}

func (g *Gateway) CreateLookalike(ctx context.Context, token, metaAccountID string, draft advertising.LookalikeDraft) (string, error) {
	path, err := accountPath(metaAccountID)
	if err != nil {
		return "", err
	}
	spec, err := jsonValue(graphLookalikeSpec{Ratio: draft.Ratio(), Country: draft.Country})
	if err != nil {
		return "", err
	}
	form := url.Values{
		"name":               {draft.Name},
		"subtype":            {"LOOKALIKE"},
		"origin_audience_id": {draft.OriginAudienceID},
		"lookalike_spec":     {spec},
	}
	return g.created(ctx, meta.Request{Method: http.MethodPost, Path: path + "/customaudiences", Token: token, Form: form}, "lookalike audience")
}

func (g *Gateway) DeleteAudience(ctx context.Context, token, audienceID string) error {
	path, err := objectPath(audienceID)
	if err != nil {
		return err
	}
	return g.acknowledged(ctx, meta.Request{Method: http.MethodDelete, Path: path, Token: token})
}
