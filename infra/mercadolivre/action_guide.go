package mercadolivre

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	mldomain "vozko/domain/mercadolivre"
)

var reasonIDPattern = regexp.MustCompile(`^[A-Z0-9_]{1,128}$`)

var (
	errReasonOption  = errors.New("mercadolivre: a reason needs a valid option id")
	errReasonContent = errors.New("mercadolivre: a reason needs a template or a text")
)

type ActionGuideService struct {
	c *Client
}

var _ mldomain.ActionGuideService = (*ActionGuideService)(nil)

func NewActionGuideService(c *Client) (*ActionGuideService, error) {
	if c == nil {
		return nil, errors.New("mercadolivre: action guide service requires a client")
	}
	return &ActionGuideService{c: c}, nil
}

func actionGuidePath(packKey string) string {
	return "/messages/action_guide/packs/" + pathEscape(packKey)
}

func postSaleQuery() url.Values {
	q := url.Values{}
	q.Set("tag", tagPostSale)
	return q
}

type reasonTextWire struct {
	HTML string `json:"html"`
}

type reasonOptionWire struct {
	ID                  string `json:"id"`
	InternalDescription string `json:"internal_description"`
	Type                string `json:"type"`
	Enabled             bool   `json:"enabled"`
	Actionable          bool   `json:"actionable"`
	CharLimit           *int   `json:"char_limit"`
	CapAvailable        int    `json:"cap_available"`
	Templates           []struct {
		ID    string                    `json:"id"`
		Texts map[string]reasonTextWire `json:"texts"`
		Vars  []struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		} `json:"vars"`
	} `json:"templates"`
}

func textsBySite(texts map[string]reasonTextWire) map[string]string {
	var out map[string]string
	for site, text := range texts {
		if strings.TrimSpace(text.HTML) == "" {
			continue
		}
		if out == nil {
			out = make(map[string]string, len(texts))
		}
		out[strings.ToUpper(strings.TrimSpace(site))] = text.HTML
	}
	return out
}

func (s *ActionGuideService) Options(ctx context.Context, creds mldomain.Credentials, packKey string) ([]mldomain.ReasonOption, error) {
	if err := validPack(packKey); err != nil {
		return nil, err
	}
	var w struct {
		Options []reasonOptionWire `json:"options"`
	}
	if err := s.c.getJSON(ctx, request{path: actionGuidePath(packKey), query: postSaleQuery(), family: FamilyMessagesRead, creds: creds}, &w); err != nil {
		return nil, err
	}
	out := make([]mldomain.ReasonOption, 0, len(w.Options))
	for _, o := range w.Options {
		opt := mldomain.ReasonOption{
			ID:           o.ID,
			Description:  strings.TrimSpace(o.InternalDescription),
			Type:         o.Type,
			Enabled:      o.Enabled,
			Actionable:   o.Actionable,
			CapAvailable: o.CapAvailable,
		}
		if o.CharLimit != nil {
			opt.CharLimit = *o.CharLimit
		}
		for _, t := range o.Templates {
			tpl := mldomain.ReasonTemplate{ID: t.ID, TextsBySite: textsBySite(t.Texts)}
			for _, v := range t.Vars {
				tpl.Vars = append(tpl.Vars, mldomain.ReasonVar{ID: v.ID, Type: v.Type})
			}
			opt.Templates = append(opt.Templates, tpl)
		}
		out = append(out, opt)
	}
	return out, nil
}

func (s *ActionGuideService) Caps(ctx context.Context, creds mldomain.Credentials, packKey string) ([]mldomain.ReasonCap, error) {
	if err := validPack(packKey); err != nil {
		return nil, err
	}
	var w []struct {
		OptionID     string `json:"option_id"`
		CapAvailable int    `json:"cap_available"`
	}
	if err := s.c.getJSON(ctx, request{path: actionGuidePath(packKey) + "/caps_available", query: postSaleQuery(), family: FamilyMessagesRead, creds: creds}, &w); err != nil {
		return nil, err
	}
	out := make([]mldomain.ReasonCap, 0, len(w))
	for _, c := range w {
		out = append(out, mldomain.ReasonCap{OptionID: c.OptionID, CapAvailable: c.CapAvailable})
	}
	return out, nil
}

type reasonVarBody struct {
	ID    string `json:"id"`
	Value int    `json:"value"`
}

type reasonBody struct {
	OptionID   string          `json:"option_id"`
	TemplateID string          `json:"template_id,omitempty"`
	Text       string          `json:"text,omitempty"`
	Vars       []reasonVarBody `json:"vars,omitempty"`
}

func (s *ActionGuideService) SendOption(ctx context.Context, creds mldomain.Credentials, packKey string, send mldomain.ReasonSend) (*mldomain.RemoteMessage, error) {
	if err := validPack(packKey); err != nil {
		return nil, err
	}
	if !reasonIDPattern.MatchString(send.OptionID) {
		return nil, errReasonOption
	}
	body := reasonBody{OptionID: send.OptionID}
	switch {
	case send.TemplateID != "":
		if !reasonIDPattern.MatchString(send.TemplateID) {
			return nil, errReasonContent
		}
		body.TemplateID = send.TemplateID
		for _, v := range send.Vars {
			if !reasonIDPattern.MatchString(v.ID) {
				return nil, errReasonContent
			}
			body.Vars = append(body.Vars, reasonVarBody(v))
		}
	case strings.TrimSpace(send.Text) != "":
		if err := mldomain.ValidateMessageText(send.Text); err != nil {
			return nil, err
		}
		body.Text = send.Text
	default:
		return nil, errReasonContent
	}
	var w messageWire
	resp, err := s.c.sendJSON(ctx, request{
		method: http.MethodPost,
		path:   actionGuidePath(packKey) + "/option",
		query:  postSaleQuery(),
		family: FamilyMessagesWrite,
		creds:  creds,
	}, body, &w)
	if err != nil {
		return nil, markAcceptedButUnreadable(resp, err)
	}
	return w.toDomain(), nil
}
