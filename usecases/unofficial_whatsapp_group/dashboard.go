package unofficial_whatsapp_group_usecase

import (
	"context"
	"time"

	"vozko/domain/shared"
	uwg "vozko/domain/unofficial_whatsapp_group"
)

const (
	dashboardTopGroups      = 5
	dashboardTopGrowing     = 5
	dashboardSentimentLimit = 10
	dashboardInsights       = 10
	dashboardPeople         = 20
	dashboardThemes         = 8
	joinsWindow             = 7 * 24 * time.Hour
)

func (s *Service) Dashboard(ctx context.Context, a Actor) (*DashboardStats, error) {
	n, err := s.numbersOf(ctx, a)
	if err != nil {
		return nil, err
	}
	scope := uwg.StatsScope{WorkspaceID: a.WorkspaceID, Scope: n.scope}
	loc := s.location(ctx, a.WorkspaceID)
	now := s.d.Clock()
	week := uwg.LastDays(uwg.DashboardWindowDays, now, loc)
	month := uwg.LastDays(uwg.GrowthWindowDays, now, loc)

	d := &DashboardStats{Timezone: loc.String()}
	for _, instance := range n.inScope {
		d.Connections.Total++
		if instance.SessionLive() {
			d.Connections.Connected++
		}
	}
	if d.Groups, err = s.d.Stats.GroupTotals(ctx, scope); err != nil {
		return nil, err
	}
	if d.People, err = s.d.Stats.PeopleTotals(ctx, scope); err != nil {
		return nil, err
	}
	if d.Analyses.Total, d.Analyses.Active, err = s.d.Analyses.Counts(ctx, scope); err != nil {
		return nil, err
	}
	if d.Runs, err = s.d.Runs.Totals(ctx, scope); err != nil {
		return nil, err
	}
	daily, err := s.d.Stats.DailyMessages(ctx, scope, week, loc)
	if err != nil {
		return nil, err
	}
	d.DailyVolume = uwg.FillDaily(week, loc, daily)
	if d.TopGroups, err = s.d.Stats.TopGroupsByMessages(ctx, scope, week, dashboardTopGroups); err != nil {
		return nil, err
	}
	if d.Sentiment, err = s.d.Runs.SentimentTally(ctx, scope, week.From); err != nil {
		return nil, err
	}
	if d.SentimentByGroup, err = s.d.Runs.SentimentByGroup(ctx, scope, week.From, dashboardSentimentLimit); err != nil {
		return nil, err
	}
	response, err := s.d.Stats.ResponseTimes(ctx, scope, week, loc)
	if err != nil {
		return nil, err
	}
	d.ResponseTime = uwg.FillResponseTimes(week, loc, response)
	growth, err := s.d.Stats.DailyGrowth(ctx, scope, "", month, loc)
	if err != nil {
		return nil, err
	}
	d.Growth = uwg.FillDaily(month, loc, growth)
	if d.TopGrowing, err = s.d.Stats.TopGrowing(ctx, scope, month, dashboardTopGrowing); err != nil {
		return nil, err
	}
	if d.Insights, err = s.d.Runs.RecentInsights(ctx, scope, dashboardInsights); err != nil {
		return nil, err
	}
	people, err := s.d.Stats.CrossGroupPeople(ctx, uwg.PeopleFilter{StatsScope: scope, MinGroups: uwg.MinCrossGroups,
		Pagination: shared.Pagination{Page: 1, PageSize: dashboardPeople}})
	if err != nil {
		return nil, err
	}
	d.CrossGroupPeople, d.CrossGroupTotal = people.Items, people.TotalItems
	themes, err := s.d.Runs.Themes(ctx, uwg.ThemeFilter{StatsScope: scope, Since: month.From,
		Pagination: shared.Pagination{Page: 1, PageSize: dashboardThemes}})
	if err != nil {
		return nil, err
	}
	d.Themes = themes.Items
	return d, nil
}

type MembershipTableInput struct {
	Period     uwg.Period
	Origin     uwg.Origin
	Category   *string
	Search     string
	Sort       uwg.MembershipSort
	Desc       bool
	Pagination shared.Pagination
}

func (s *Service) MembershipTable(ctx context.Context, a Actor, in MembershipTableInput) (*uwg.MembershipPage, error) {
	n, err := s.numbersOf(ctx, a)
	if err != nil {
		return nil, err
	}
	loc := s.location(ctx, a.WorkspaceID)
	if in.Period == "" {
		in.Period = uwg.Period30d
	}
	window, err := uwg.ResolveWindow(in.Period, s.d.Clock(), loc)
	if err != nil {
		return nil, err
	}
	rows, err := s.d.Stats.Membership(ctx, uwg.MembershipFilter{
		StatsScope: uwg.StatsScope{WorkspaceID: a.WorkspaceID, Scope: n.scope},
		Window:     window, Origin: in.Origin, Category: in.Category, Search: in.Search,
	})
	if err != nil {
		return nil, err
	}
	page := uwg.BuildMembershipPage(rows, in.Sort, in.Desc, in.Pagination)
	page.From, page.To, page.Timezone = window.From, window.To, loc.String()
	return &page, nil
}

func (s *Service) People(ctx context.Context, a Actor, search string, minGroups int, p shared.Pagination) (*shared.PaginatedResult[*uwg.Person], error) {
	n, err := s.numbersOf(ctx, a)
	if err != nil {
		return nil, err
	}
	return s.d.Stats.CrossGroupPeople(ctx, uwg.PeopleFilter{
		StatsScope: uwg.StatsScope{WorkspaceID: a.WorkspaceID, Scope: n.scope},
		Search:     search, MinGroups: uwg.CrossGroupMinimum(minGroups), Pagination: p,
	})
}

func (s *Service) Themes(ctx context.Context, a Actor, days int, search string, p shared.Pagination) (*shared.PaginatedResult[*uwg.ThemeAggregate], error) {
	n, err := s.numbersOf(ctx, a)
	if err != nil {
		return nil, err
	}
	if days <= 0 {
		days = uwg.OverviewWindowDays
	}
	window := uwg.LastDays(days, s.d.Clock(), s.location(ctx, a.WorkspaceID))
	return s.d.Runs.Themes(ctx, uwg.ThemeFilter{
		StatsScope: uwg.StatsScope{WorkspaceID: a.WorkspaceID, Scope: n.scope},
		Since:      window.From, Search: search, Pagination: p,
	})
}

func (s *Service) Joins(ctx context.Context, a Actor) ([]uwg.JoinTask, error) {
	n, err := s.numbersOf(ctx, a)
	if err != nil {
		return nil, err
	}
	return s.d.Joins.Recent(ctx, a.WorkspaceID, n.scope, s.d.Clock().Add(-joinsWindow))
}
