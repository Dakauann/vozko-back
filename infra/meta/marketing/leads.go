package marketing

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"vozko/domain/advertising"
	"vozko/infra/meta"
)

var _ advertising.LeadGateway = (*Gateway)(nil)

const (
	leadgenField         = "leadgen"
	leadFormFields       = "id,name,status,locale,questions,privacy_policy_url,leads_count,created_time"
	leadFields           = "id,created_time,ad_id,form_id,field_data"
	answerSeparator      = ", "
	thankYouButton       = "VIEW_WEBSITE"
	leadCreatedFilterKey = "time_created"
)

func (g *Gateway) pageToken(ctx context.Context, token, pageID string) (string, string, error) {
	path, err := objectPath(pageID)
	if err != nil {
		return "", "", err
	}
	pageToken, err := g.pageAccessToken(ctx, token, pageID)
	if err != nil {
		return "", "", err
	}
	return path, pageToken, nil
}

type graphFormOption struct {
	Value string `json:"value"`
	Key   string `json:"key"`
}

type graphFormQuestion struct {
	Type    advertising.QuestionType `json:"type"`
	Key     string                   `json:"key,omitempty"`
	Label   string                   `json:"label,omitempty"`
	Options []graphFormOption        `json:"options,omitempty"`
}

type graphLeadForm struct {
	ID          meta.GraphID        `json:"id"`
	Name        string              `json:"name"`
	Status      string              `json:"status"`
	Locale      string              `json:"locale"`
	Questions   []graphFormQuestion `json:"questions"`
	PrivacyURL  string              `json:"privacy_policy_url"`
	LeadsCount  graphNumber         `json:"leads_count"`
	CreatedTime string              `json:"created_time"`
}

func (g *Gateway) ListForms(ctx context.Context, token, pageID string) ([]advertising.LeadForm, error) {
	path, pageToken, err := g.pageToken(ctx, token, pageID)
	if err != nil {
		return nil, err
	}
	rows, err := collect[graphLeadForm](ctx, g, path+"/leadgen_forms", pageToken, url.Values{"fields": {leadFormFields}, "limit": {"100"}})
	if err != nil {
		return nil, err
	}
	forms := make([]advertising.LeadForm, 0, len(rows))
	for _, row := range rows {
		form, err := row.toDomain(pageID)
		if err != nil {
			return nil, err
		}
		forms = append(forms, form)
	}
	return forms, nil
}

func (r graphLeadForm) toDomain(pageID string) (advertising.LeadForm, error) {
	if r.ID == "" {
		return advertising.LeadForm{}, fmt.Errorf("marketing: lead form without an id")
	}
	count, err := r.LeadsCount.wholePart("leads_count")
	if err != nil {
		return advertising.LeadForm{}, err
	}
	created, err := graphTime("created_time", r.CreatedTime)
	if err != nil {
		return advertising.LeadForm{}, err
	}
	questions := make([]advertising.FormQuestion, 0, len(r.Questions))
	for _, q := range r.Questions {
		question := advertising.FormQuestion{Type: q.Type, Label: q.Label}
		if q.Type == advertising.QuestionCustom {
			question.Key = q.Key
		}
		for _, option := range q.Options {
			question.Options = append(question.Options, option.Value)
		}
		questions = append(questions, question)
	}
	return advertising.LeadForm{
		MetaID:      r.ID.String(),
		PageID:      pageID,
		Name:        r.Name,
		Status:      advertising.FormStatus(r.Status),
		Locale:      r.Locale,
		Questions:   questions,
		PrivacyURL:  r.PrivacyURL,
		LeadsCount:  count,
		CreatedTime: created,
	}, nil
}

type graphPrivacyPolicy struct {
	URL      string `json:"url"`
	LinkText string `json:"link_text,omitempty"`
}

type graphThankYouPage struct {
	Title      string `json:"title,omitempty"`
	Body       string `json:"body,omitempty"`
	ButtonType string `json:"button_type,omitempty"`
	ButtonText string `json:"button_text,omitempty"`
	WebsiteURL string `json:"website_url,omitempty"`
}

type graphContextCard struct {
	Title   string   `json:"title"`
	Style   string   `json:"style"`
	Content []string `json:"content"`
}

var contextCardStyles = map[advertising.IntroStyle]string{
	advertising.IntroParagraph: "PARAGRAPH_STYLE",
	advertising.IntroList:      "LIST_STYLE",
}

func contextCard(intro advertising.FormIntro) (string, error) {
	style, ok := contextCardStyles[intro.Style]
	if !ok {
		return "", fmt.Errorf("marketing: intro style %q has no meta context card style", intro.Style)
	}
	return jsonValue(graphContextCard{Title: intro.Title, Style: style, Content: intro.Lines()})
}

func formQuestions(questions []advertising.FormQuestion) []graphFormQuestion {
	out := make([]graphFormQuestion, 0, len(questions))
	for _, q := range questions {
		if q.Type != advertising.QuestionCustom {
			out = append(out, graphFormQuestion{Type: q.Type})
			continue
		}
		question := graphFormQuestion{Type: q.Type, Key: q.Key, Label: q.Label}
		for _, option := range q.Options {
			question.Options = append(question.Options, graphFormOption{Value: option, Key: option})
		}
		out = append(out, question)
	}
	return out
}

func (g *Gateway) CreateForm(ctx context.Context, token string, draft advertising.LeadFormDraft) (string, error) {
	path, pageToken, err := g.pageToken(ctx, token, draft.PageID)
	if err != nil {
		return "", err
	}
	questions, err := jsonValue(formQuestions(draft.Questions))
	if err != nil {
		return "", err
	}
	privacy, err := jsonValue(graphPrivacyPolicy{URL: draft.PrivacyURL, LinkText: draft.PrivacyText})
	if err != nil {
		return "", err
	}
	thanks, err := jsonValue(graphThankYouPage{Title: draft.ThankYouTitle, Body: draft.ThankYouBody, ButtonType: thankYouButton, ButtonText: draft.ThankYouButtonText, WebsiteURL: draft.ThankYouURL})
	if err != nil {
		return "", err
	}
	form := url.Values{
		"name":                 {draft.Name},
		"locale":               {draft.Locale},
		"questions":            {questions},
		"privacy_policy":       {privacy},
		"thank_you_page":       {thanks},
		"follow_up_action_url": {draft.ThankYouURL},
	}
	if draft.Intro != nil {
		card, err := contextCard(*draft.Intro)
		if err != nil {
			return "", err
		}
		form.Set("context_card", card)
	}
	if draft.HigherIntent {
		form.Set("is_optimized_for_quality", "true")
	}
	return g.created(ctx, meta.Request{Method: http.MethodPost, Path: path + "/leadgen_forms", Token: pageToken, Form: form}, "lead form")
}

func (g *Gateway) ArchiveForm(ctx context.Context, token, pageID, formID string) error {
	formPath, err := objectPath(formID)
	if err != nil {
		return err
	}
	_, pageToken, err := g.pageToken(ctx, token, pageID)
	if err != nil {
		return err
	}
	form := url.Values{"status": {string(advertising.FormArchived)}}
	return g.acknowledged(ctx, meta.Request{Method: http.MethodPost, Path: formPath, Token: pageToken, Form: form})
}

type graphLead struct {
	ID          meta.GraphID `json:"id"`
	CreatedTime string       `json:"created_time"`
	AdID        meta.GraphID `json:"ad_id"`
	FormID      meta.GraphID `json:"form_id"`
	FieldData   []struct {
		Name   string   `json:"name"`
		Values []string `json:"values"`
	} `json:"field_data"`
}

func (r graphLead) toDomain(pageID string) (advertising.FormLead, error) {
	if r.ID == "" {
		return advertising.FormLead{}, fmt.Errorf("marketing: lead without an id")
	}
	created, err := graphTime("created_time", r.CreatedTime)
	if err != nil {
		return advertising.FormLead{}, err
	}
	if created == nil {
		return advertising.FormLead{}, fmt.Errorf("marketing: lead %s without a created time", r.ID)
	}
	answers := make(map[string]string, len(r.FieldData))
	for _, field := range r.FieldData {
		answers[field.Name] = strings.Join(field.Values, answerSeparator)
	}
	return advertising.FormLead{
		MetaID:      r.ID.String(),
		FormMetaID:  r.FormID.String(),
		AdMetaID:    r.AdID.String(),
		PageID:      pageID,
		Answers:     answers,
		CreatedTime: *created,
	}, nil
}

type graphLeadFilter struct {
	Field    string `json:"field"`
	Operator string `json:"operator"`
	Value    int64  `json:"value"`
}

func (g *Gateway) ListLeads(ctx context.Context, token, pageID, formID string, since time.Time) ([]advertising.FormLead, error) {
	formPath, err := objectPath(formID)
	if err != nil {
		return nil, err
	}
	_, pageToken, err := g.pageToken(ctx, token, pageID)
	if err != nil {
		return nil, err
	}
	query := url.Values{"fields": {leadFields}, "limit": {"100"}}
	if !since.IsZero() {
		filtering, err := jsonValue([]graphLeadFilter{{Field: leadCreatedFilterKey, Operator: "GREATER_THAN", Value: since.Unix()}})
		if err != nil {
			return nil, err
		}
		query.Set("filtering", filtering)
	}
	rows, err := collect[graphLead](ctx, g, formPath+"/leads", pageToken, query)
	if err != nil {
		return nil, err
	}
	leads := make([]advertising.FormLead, 0, len(rows))
	for _, row := range rows {
		lead, err := row.toDomain(pageID)
		if err != nil {
			return nil, err
		}
		leads = append(leads, lead)
	}
	return leads, nil
}

func (g *Gateway) GetLead(ctx context.Context, token, pageID, leadgenID string) (*advertising.FormLead, error) {
	leadPath, err := objectPath(leadgenID)
	if err != nil {
		return nil, err
	}
	_, pageToken, err := g.pageToken(ctx, token, pageID)
	if err != nil {
		return nil, err
	}
	var row graphLead
	if err := g.do(ctx, meta.Request{Method: http.MethodGet, Path: leadPath, Token: pageToken, Query: url.Values{"fields": {leadFields}}}, &row); err != nil {
		return nil, err
	}
	lead, err := row.toDomain(pageID)
	if err != nil {
		return nil, err
	}
	return &lead, nil
}

func (g *Gateway) SubscribeLeadgen(ctx context.Context, token, pageID string) error {
	path, pageToken, err := g.pageToken(ctx, token, pageID)
	if err != nil {
		return err
	}
	var out struct {
		Data []struct {
			SubscribedFields []string `json:"subscribed_fields"`
		} `json:"data"`
	}
	if err := g.do(ctx, meta.Request{Method: http.MethodGet, Path: path + "/subscribed_apps", Token: pageToken}, &out); err != nil {
		return err
	}
	if len(out.Data) > 1 {
		return fmt.Errorf("marketing: page %s listed %d subscribed apps for the page token, expected only ours", pageID, len(out.Data))
	}
	var fields []string
	if len(out.Data) == 1 {
		fields = out.Data[0].SubscribedFields
	}
	if slices.Contains(fields, leadgenField) {
		return nil
	}
	fields = append(slices.Clone(fields), leadgenField)
	form := url.Values{"subscribed_fields": {strings.Join(fields, ",")}}
	return g.acknowledged(ctx, meta.Request{Method: http.MethodPost, Path: path + "/subscribed_apps", Token: pageToken, Form: form})
}
