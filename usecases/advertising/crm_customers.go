package advertising

import (
	"context"
	"strings"

	ads "vozko/domain/advertising"
	"vozko/domain/crmfilter"
	"vozko/domain/lead"
	"vozko/domain/shared"
)

const crmCustomerPage = 1000

type leadLister interface {
	List(input lead.ListLeadsInput) (*shared.PaginatedResult[*lead.Lead], error)
}

type crmCustomers struct {
	leads leadLister
}

func NewCRMCustomers(leads leadLister) CustomerDirectory {
	return &crmCustomers{leads: leads}
}

func (c *crmCustomers) Customers(_ context.Context, workspaceID string, filter crmfilter.Filter, limit int) ([]ads.Customer, error) {
	var out []ads.Customer
	for page := 1; len(out) < limit; page++ {
		result, err := c.leads.List(lead.ListLeadsInput{
			WorkspaceID: workspaceID,
			Filter:      filter,
			Options:     shared.QueryOptions{Pagination: shared.Pagination{Page: page, PageSize: crmCustomerPage}},
		})
		if err != nil {
			return nil, err
		}
		for _, l := range result.Items {
			if l == nil || l.Blocked {
				continue
			}
			out = append(out, customerOf(l))
		}
		if len(result.Items) < crmCustomerPage {
			break
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func customerOf(l *lead.Lead) ads.Customer {
	first, last, _ := strings.Cut(strings.TrimSpace(l.Name), " ")
	return ads.Customer{ads.MatchPhone: l.Number, ads.MatchFirstName: first, ads.MatchLastName: last}
}

type libraryFiles struct {
	media   mediaReader
	fetcher fileFetcher
}

func NewLibraryFiles(media mediaReader, fetcher fileFetcher) RawFiles {
	return &libraryFiles{media: media, fetcher: fetcher}
}

func (f *libraryFiles) Bytes(ctx context.Context, workspaceID, mediaID string) ([]byte, error) {
	m, err := f.media.GetMedia(workspaceID, strings.TrimSpace(mediaID))
	if err != nil {
		return nil, err
	}
	if m == nil || strings.TrimSpace(m.URL) == "" {
		return nil, ErrCustomerFileUnreadable
	}
	return f.fetcher.Fetch(ctx, m.URL)
}
