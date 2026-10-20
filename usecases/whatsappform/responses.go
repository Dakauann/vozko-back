package whatsappform_usecase

import (
	"context"
	"time"

	wf "vozko/domain/whatsappform"
)

type ResponseRow struct {
	Response wf.Response `json:"response"`
	LeadName string      `json:"leadName,omitempty"`
}

type FormRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type ResponseDetail struct {
	Response wf.Response `json:"response"`
	Form     *FormRef    `json:"form,omitempty"`
	Version  *wf.Version `json:"version,omitempty"`
	LeadName string      `json:"leadName,omitempty"`
	Revealed bool        `json:"revealed"`
}

type Period struct {
	From, To *time.Time
}

type versionCache struct {
	svc         *Service
	workspaceID string
	byID        map[string]*wf.Version
}

func (c *versionCache) get(ctx context.Context, id string) (*wf.Version, error) {
	if v, ok := c.byID[id]; ok {
		return v, nil
	}
	v, err := c.svc.deps.Versions.Get(ctx, c.workspaceID, id)
	if err != nil {
		return nil, err
	}
	c.byID[id] = v
	return v, nil
}

func (s *Service) shown(ctx context.Context, cache *versionCache, resp wf.Response, reveal bool) (wf.Response, *wf.Version, error) {
	if resp.VersionID == "" {
		return resp, nil, nil
	}
	version, err := cache.get(ctx, resp.VersionID)
	if err != nil {
		return wf.Response{}, nil, err
	}
	if !reveal {
		resp.Answers = wf.MaskSensitive(version.Document, resp.Answers)
	}
	return resp, version, nil
}

func (s *Service) readerScope(a Actor) (wf.ReadScope, error) {
	if err := s.require(a, wf.CapabilityView, wf.CapabilityResponses); err != nil {
		return wf.ReadScope{}, err
	}
	return s.readScope(a)
}

func (s *Service) ListResponses(ctx context.Context, a Actor, q wf.ResponseQuery) (wf.Page[ResponseRow], error) {
	scope, err := s.readerScope(a)
	if err != nil {
		return wf.Page[ResponseRow]{}, err
	}
	if _, err := s.deps.Forms.Get(ctx, a.WorkspaceID, q.FormID); err != nil {
		return wf.Page[ResponseRow]{}, err
	}
	q.WorkspaceID, q.Scope = a.WorkspaceID, scope
	if err := q.Normalize(); err != nil {
		return wf.Page[ResponseRow]{}, err
	}
	page, err := s.deps.Responses.List(ctx, q)
	if err != nil {
		return wf.Page[ResponseRow]{}, err
	}
	names, err := s.leadNamesOf(ctx, a.WorkspaceID, page.Items)
	if err != nil {
		return wf.Page[ResponseRow]{}, err
	}
	reveal := s.holds(a, wf.CapabilityResponsesSensitive)
	cache := &versionCache{svc: s, workspaceID: a.WorkspaceID, byID: map[string]*wf.Version{}}
	out := wf.Page[ResponseRow]{Items: make([]ResponseRow, 0, len(page.Items)), Next: page.Next}
	for _, resp := range page.Items {
		shown, _, err := s.shown(ctx, cache, resp, reveal)
		if err != nil {
			return wf.Page[ResponseRow]{}, err
		}
		out.Items = append(out.Items, ResponseRow{Response: shown, LeadName: names[resp.LeadID]})
	}
	return out, nil
}

func (s *Service) leadNamesOf(ctx context.Context, workspaceID string, rows []wf.Response) (map[string]string, error) {
	var ids []string
	seen := map[string]bool{}
	for _, r := range rows {
		if r.LeadID != "" && !seen[r.LeadID] {
			seen[r.LeadID] = true
			ids = append(ids, r.LeadID)
		}
	}
	if len(ids) == 0 {
		return map[string]string{}, nil
	}
	return s.deps.LeadNames.Names(ctx, workspaceID, ids)
}

func (s *Service) GetResponse(ctx context.Context, a Actor, responseID string) (ResponseDetail, error) {
	scope, err := s.readerScope(a)
	if err != nil {
		return ResponseDetail{}, err
	}
	resp, err := s.deps.Responses.Get(ctx, a.WorkspaceID, responseID, scope)
	if err != nil {
		return ResponseDetail{}, err
	}
	reveal := s.holds(a, wf.CapabilityResponsesSensitive)
	cache := &versionCache{svc: s, workspaceID: a.WorkspaceID, byID: map[string]*wf.Version{}}
	shown, version, err := s.shown(ctx, cache, *resp, reveal)
	if err != nil {
		return ResponseDetail{}, err
	}
	out := ResponseDetail{Response: shown, Version: version, Revealed: reveal}
	if resp.FormID != "" {
		form, err := s.deps.Forms.Get(ctx, a.WorkspaceID, resp.FormID)
		if err != nil {
			return ResponseDetail{}, err
		}
		out.Form = &FormRef{ID: form.ID, Name: form.Name}
	}
	names, err := s.leadNamesOf(ctx, a.WorkspaceID, []wf.Response{*resp})
	if err != nil {
		return ResponseDetail{}, err
	}
	out.LeadName = names[resp.LeadID]
	return out, nil
}

func (s *Service) DeleteResponse(ctx context.Context, a Actor, responseID string) error {
	if err := s.require(a, wf.CapabilityResponsesDelete); err != nil {
		return err
	}
	scope, err := s.readerScope(a)
	if err != nil {
		return err
	}
	if _, err := s.deps.Responses.Get(ctx, a.WorkspaceID, responseID, scope); err != nil {
		return err
	}
	return s.deps.Responses.Delete(ctx, a.WorkspaceID, responseID)
}

func (s *Service) statsQuery(ctx context.Context, a Actor, formID string, p Period) (wf.StatsQuery, error) {
	scope, err := s.readerScope(a)
	if err != nil {
		return wf.StatsQuery{}, err
	}
	if _, err := s.deps.Forms.Get(ctx, a.WorkspaceID, formID); err != nil {
		return wf.StatsQuery{}, err
	}
	if p.From != nil && p.To != nil && p.From.After(*p.To) {
		return wf.StatsQuery{}, wf.ErrListQueryInvalid
	}
	return wf.StatsQuery{WorkspaceID: a.WorkspaceID, FormID: formID, From: p.From, To: p.To, Scope: scope}, nil
}

func (s *Service) Stats(ctx context.Context, a Actor, formID string, p Period) (wf.Stats, error) {
	q, err := s.statsQuery(ctx, a, formID, p)
	if err != nil {
		return wf.Stats{}, err
	}
	return s.deps.Responses.Stats(ctx, q)
}

func (s *Service) Distribution(ctx context.Context, a Actor, formID, fieldKey string, p Period) (map[string]int64, error) {
	q, err := s.statsQuery(ctx, a, formID, p)
	if err != nil {
		return nil, err
	}
	return s.deps.Responses.Distribution(ctx, q, fieldKey)
}
