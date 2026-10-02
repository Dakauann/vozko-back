package marketing

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"vozko/domain/advertising"
	"vozko/infra/meta"
)

const (
	adAccountFields = "account_id,name,currency,timezone_name,account_status,disable_reason,funding_source,amount_spent,spend_cap,business{id,name},user_tasks"
	pageFields      = "id,name,tasks,picture{url},whatsapp_number,leadgen_tos_accepted,instagram_business_account{id,username}"
)

var _ advertising.StructureGateway = (*Gateway)(nil)

type graphAdAccount struct {
	AccountID     meta.GraphID `json:"account_id"`
	Name          string       `json:"name"`
	Currency      string       `json:"currency"`
	TimezoneName  string       `json:"timezone_name"`
	AccountStatus int          `json:"account_status"`
	DisableReason int          `json:"disable_reason"`
	FundingSource meta.GraphID `json:"funding_source"`
	AmountSpent   graphNumber  `json:"amount_spent"`
	SpendCap      graphNumber  `json:"spend_cap"`
	Business      *graphRef    `json:"business"`
	UserTasks     []string     `json:"user_tasks"`
}

func (a graphAdAccount) toDomain() (advertising.RemoteAdAccount, error) {
	if a.AccountID == "" {
		return advertising.RemoteAdAccount{}, fmt.Errorf("marketing: ad account without account_id")
	}
	spent, err := a.AmountSpent.minorUnits("amount_spent")
	if err != nil {
		return advertising.RemoteAdAccount{}, err
	}
	spendCap, err := a.SpendCap.minorUnits("spend_cap")
	if err != nil {
		return advertising.RemoteAdAccount{}, err
	}
	out := advertising.RemoteAdAccount{
		MetaAccountID: advertising.NormalizeAccountID(a.AccountID.String()),
		Name:          a.Name,
		Currency:      a.Currency,
		Timezone:      a.TimezoneName,
		Status:        advertising.MetaAccountStatus(a.AccountStatus),
		DisableReason: a.DisableReason,
		HasFunding:    a.FundingSource != "",
		AmountSpent:   spent,
		SpendCap:      spendCap,
		Tasks:         a.UserTasks,
	}
	if a.Business != nil {
		out.BusinessID = a.Business.ID.String()
		out.BusinessName = a.Business.Name
	}
	return out, nil
}

func (g *Gateway) ListAdAccounts(ctx context.Context, token string) ([]advertising.RemoteAdAccount, error) {
	q := url.Values{}
	q.Set("fields", adAccountFields)
	q.Set("limit", "100")
	rows, err := collect[graphAdAccount](ctx, g, "/me/adaccounts", token, q)
	if err != nil {
		return nil, err
	}
	accounts := make([]advertising.RemoteAdAccount, 0, len(rows))
	for _, row := range rows {
		account, err := row.toDomain()
		if err != nil {
			return nil, err
		}
		accounts = append(accounts, account)
	}
	return accounts, nil
}

func (g *Gateway) GetAdAccount(ctx context.Context, token, metaAccountID string) (*advertising.RemoteAdAccount, error) {
	path, err := accountPath(metaAccountID)
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	q.Set("fields", adAccountFields)
	var row graphAdAccount
	if err := g.do(ctx, meta.Request{Method: http.MethodGet, Path: path, Token: token, Query: q}, &row); err != nil {
		return nil, err
	}
	account, err := row.toDomain()
	if err != nil {
		return nil, err
	}
	return &account, nil
}

type graphPageRow struct {
	ID      meta.GraphID `json:"id"`
	Name    string       `json:"name"`
	Tasks   []string     `json:"tasks"`
	Picture *struct {
		Data struct {
			URL string `json:"url"`
		} `json:"data"`
	} `json:"picture"`
	WhatsAppNumber     string `json:"whatsapp_number"`
	LeadgenTOSAccepted bool   `json:"leadgen_tos_accepted"`
	Instagram          *struct {
		ID       meta.GraphID `json:"id"`
		Username string       `json:"username"`
	} `json:"instagram_business_account"`
}

func (p graphPageRow) toDomain() advertising.RemotePage {
	out := advertising.RemotePage{
		PageID:            p.ID.String(),
		Name:              p.Name,
		WhatsAppNumber:    p.WhatsAppNumber,
		CanAdvertise:      advertising.TasksAllowAdvertising(p.Tasks),
		LeadTermsAccepted: p.LeadgenTOSAccepted,
	}
	if p.Picture != nil {
		out.PictureURL = p.Picture.Data.URL
	}
	if p.Instagram != nil {
		out.InstagramUserID = p.Instagram.ID.String()
		out.InstagramUsername = p.Instagram.Username
	}
	return out
}

func (g *Gateway) ListPages(ctx context.Context, token string) ([]advertising.RemotePage, error) {
	q := url.Values{}
	q.Set("fields", pageFields)
	q.Set("limit", "100")
	rows, err := collect[graphPageRow](ctx, g, "/me/accounts", token, q)
	if err != nil {
		return nil, err
	}
	pages := make([]advertising.RemotePage, 0, len(rows))
	for _, row := range rows {
		if row.ID == "" {
			return nil, fmt.Errorf("marketing: page without id")
		}
		pages = append(pages, row.toDomain())
	}
	return pages, nil
}

type graphAccountMinimum struct {
	Currency       string      `json:"currency"`
	MinDailyBudget graphNumber `json:"min_daily_budget"`
}

type graphMinimumBudget struct {
	Currency      string      `json:"currency"`
	Impressions   graphNumber `json:"min_daily_budget_imp"`
	VideoViews    graphNumber `json:"min_daily_budget_video_views"`
	HighFrequency graphNumber `json:"min_daily_budget_high_freq"`
	LowFrequency  graphNumber `json:"min_daily_budget_low_freq"`
}

func (m graphMinimumBudget) applyTo(out *advertising.MinimumBudgets) error {
	for _, field := range []struct {
		name  string
		value graphNumber
		into  *int64
	}{
		{"min_daily_budget_imp", m.Impressions, &out.Impressions},
		{"min_daily_budget_video_views", m.VideoViews, &out.VideoViews},
		{"min_daily_budget_high_freq", m.HighFrequency, &out.HighFrequency},
		{"min_daily_budget_low_freq", m.LowFrequency, &out.LowFrequency},
	} {
		amount, err := field.value.minorUnits(field.name)
		if err != nil {
			return err
		}
		*field.into = amount
	}
	return nil
}

func (g *Gateway) MinimumBudgets(ctx context.Context, token, metaAccountID string, bidAmount int64) (advertising.MinimumBudgets, error) {
	path, err := accountPath(metaAccountID)
	if err != nil {
		return advertising.MinimumBudgets{}, err
	}
	q := url.Values{}
	q.Set("fields", "currency,min_daily_budget")
	var account graphAccountMinimum
	if err := g.do(ctx, meta.Request{Method: http.MethodGet, Path: path, Token: token, Query: q}, &account); err != nil {
		return advertising.MinimumBudgets{}, err
	}
	floor, err := account.MinDailyBudget.minorUnits("min_daily_budget")
	if err != nil {
		return advertising.MinimumBudgets{}, err
	}
	out := advertising.MinimumBudgets{Currency: strings.ToUpper(strings.TrimSpace(account.Currency)), Account: floor}
	edge := url.Values{}
	if bidAmount > 0 {
		edge.Set("bid_amount", strconv.FormatInt(bidAmount, 10))
	}
	rows, err := collect[graphMinimumBudget](ctx, g, path+"/minimum_budgets", token, edge)
	if err != nil {
		return advertising.MinimumBudgets{}, err
	}
	for _, row := range rows {
		if strings.EqualFold(strings.TrimSpace(row.Currency), out.Currency) {
			return out, row.applyTo(&out)
		}
	}
	return out, nil
}
