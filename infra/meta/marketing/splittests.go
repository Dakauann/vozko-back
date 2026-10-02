package marketing

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"vozko/domain/advertising"
	"vozko/infra/meta"
)

const splitTestType = "SPLIT_TEST"

var _ advertising.TestGateway = (*Gateway)(nil)

type graphTestCell struct {
	Name                string              `json:"name"`
	TreatmentPercentage int                 `json:"treatment_percentage"`
	AdSets              graphList[graphRef] `json:"adsets,omitempty"`
	Campaigns           graphList[graphRef] `json:"campaigns,omitempty"`
}

func cellsOf(test advertising.SplitTest) ([]graphTestCell, error) {
	cells := make([]graphTestCell, 0, len(test.Cells))
	for _, c := range test.Cells {
		refs := make([]graphRef, 0, len(c.ObjectIDs))
		for _, id := range c.ObjectIDs {
			refs = append(refs, graphRef{ID: meta.GraphID(id)})
		}
		cell := graphTestCell{Name: c.Name, TreatmentPercentage: c.Share}
		switch test.Level {
		case advertising.TestAdSets:
			cell.AdSets = refs
		case advertising.TestCampaigns:
			cell.Campaigns = refs
		default:
			return nil, fmt.Errorf("marketing: unknown split test level %q", test.Level)
		}
		cells = append(cells, cell)
	}
	return cells, nil
}

func (g *Gateway) CreateSplitTest(ctx context.Context, token, metaAccountID string, test advertising.SplitTest) (string, error) {
	cells, err := cellsOf(test)
	if err != nil {
		return "", err
	}
	encoded, err := jsonValue(cells)
	if err != nil {
		return "", err
	}
	account, err := g.GetAdAccount(ctx, token, metaAccountID)
	if err != nil {
		return "", err
	}
	path, err := objectPath(account.BusinessID)
	if err != nil {
		return "", fmt.Errorf("marketing: ad account %s has no business for split tests", metaAccountID)
	}
	form := url.Values{}
	form.Set("name", test.Name)
	form.Set("type", splitTestType)
	form.Set("start_time", strconv.FormatInt(test.StartAt.Unix(), 10))
	form.Set("end_time", strconv.FormatInt(test.EndAt.Unix(), 10))
	form.Set("cells", encoded)
	if test.Description != "" {
		form.Set("description", test.Description)
	}
	return g.created(ctx, meta.Request{Method: http.MethodPost, Path: path + "/ad_studies", Token: token, Form: form}, "split test")
}

type graphStudy struct {
	ID          meta.GraphID             `json:"id"`
	Name        string                   `json:"name"`
	Description string                   `json:"description"`
	Type        string                   `json:"type"`
	StartTime   string                   `json:"start_time"`
	EndTime     string                   `json:"end_time"`
	Cells       graphList[graphTestCell] `json:"cells"`
}

func (s graphStudy) toDomain() (advertising.SplitTest, error) {
	if s.ID == "" {
		return advertising.SplitTest{}, fmt.Errorf("marketing: split test %q without id", s.Name)
	}
	start, err := graphTime("start_time", s.StartTime)
	if err != nil {
		return advertising.SplitTest{}, err
	}
	end, err := graphTime("end_time", s.EndTime)
	if err != nil {
		return advertising.SplitTest{}, err
	}
	if start == nil || end == nil {
		return advertising.SplitTest{}, fmt.Errorf("marketing: split test %s without its window", s.ID)
	}
	out := advertising.SplitTest{MetaID: s.ID.String(), Name: s.Name, Description: s.Description, StartAt: *start, EndAt: *end}
	for _, c := range s.Cells {
		level := advertising.TestAdSets
		refs := c.AdSets
		switch {
		case len(c.AdSets) > 0 && len(c.Campaigns) > 0:
			return advertising.SplitTest{}, fmt.Errorf("marketing: split test %s mixes campaigns and ad sets", s.ID)
		case len(c.Campaigns) > 0:
			level, refs = advertising.TestCampaigns, c.Campaigns
		case len(c.AdSets) == 0:
			return advertising.SplitTest{}, fmt.Errorf("marketing: split test %s has an empty cell", s.ID)
		}
		if out.Level != "" && out.Level != level {
			return advertising.SplitTest{}, fmt.Errorf("marketing: split test %s mixes campaigns and ad sets", s.ID)
		}
		out.Level = level
		out.Cells = append(out.Cells, advertising.TestCell{Name: c.Name, ObjectIDs: graphIDs(refs), Share: c.TreatmentPercentage})
	}
	return out, nil
}

func (g *Gateway) ListSplitTests(ctx context.Context, token, metaAccountID string) ([]advertising.SplitTest, error) {
	path, err := accountPath(metaAccountID)
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	q.Set("fields", "id,name,description,type,start_time,end_time,cells{name,treatment_percentage,adsets,campaigns}")
	q.Set("limit", "100")
	rows, err := collect[graphStudy](ctx, g, path+"/ad_studies", token, q)
	if err != nil {
		return nil, err
	}
	tests := make([]advertising.SplitTest, 0, len(rows))
	for _, row := range rows {
		if row.Type != splitTestType {
			continue
		}
		test, err := row.toDomain()
		if err != nil {
			return nil, err
		}
		tests = append(tests, test)
	}
	return tests, nil
}
