package campaignaudience_usecase

import (
	"context"

	"vozko/domain/cache"
	"vozko/domain/campaignaudience"
)

type LeadsQuery struct {
	Filter campaignaudience.LeadFilter
	Page   int
	Limit  int
}

type LeadsPage struct {
	Items []campaignaudience.AudienceLead
	Page  int
	Limit int
	Total int
}

func (s *Service) Leads(ctx context.Context, a Actor, ch campaignaudience.Channel, params Params, spec campaignaudience.Spec, q LeadsQuery) (LeadsPage, error) {
	if err := validPage(q.Page, q.Limit); err != nil {
		return LeadsPage{}, err
	}
	if err := q.Filter.Validate(); err != nil {
		return LeadsPage{}, err
	}
	var found pager[campaignaudience.AudienceLead]
	err := cache.Gated(ctx, s.deps.Gate, func(ctx context.Context) error {
		found = newPager[campaignaudience.AudienceLead](q.Page, q.Limit)
		_, _, err := s.walk(ctx, walk{actor: a, channel: ch, params: params, spec: spec}, func(c campaignaudience.Candidate, d campaignaudience.Decision) error {
			if lead := campaignaudience.LeadOf(c, d); q.Filter.Matches(lead) {
				found.add(lead)
			}
			return nil
		})
		return err
	})
	if err != nil {
		return LeadsPage{}, err
	}
	return LeadsPage{Items: found.items, Page: q.Page, Limit: q.Limit, Total: found.total}, nil
}

func validPage(page, limit int) error {
	if page < 1 || limit < 1 || limit > campaignaudience.MaxRowsPage {
		return ErrPageInvalid
	}
	return nil
}

type pager[T any] struct {
	offset int
	limit  int
	total  int
	items  []T
}

func newPager[T any](page, limit int) pager[T] {
	return pager[T]{offset: (page - 1) * limit, limit: limit, items: []T{}}
}

func (p *pager[T]) add(item T) {
	if p.total >= p.offset && len(p.items) < p.limit {
		p.items = append(p.items, item)
	}
	p.total++
}
