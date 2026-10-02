package marketing

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"vozko/domain/advertising"
	"vozko/infra/meta"
)

const (
	adAccountFields = "account_id,name,currency,timezone_name,account_status,disable_reason,funding_source,amount_spent,spend_cap,business{id,name}"
	pageFields      = "id,name,tasks,picture{url},whatsapp_number,instagram_business_account{id,username}"
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
	WhatsAppNumber string `json:"whatsapp_number"`
	Instagram      *struct {
		ID       meta.GraphID `json:"id"`
		Username string       `json:"username"`
	} `json:"instagram_business_account"`
}

func (p graphPageRow) toDomain() advertising.RemotePage {
	out := advertising.RemotePage{
		PageID:         p.ID.String(),
		Name:           p.Name,
		WhatsAppNumber: p.WhatsAppNumber,
		CanAdvertise:   slices.ContainsFunc(p.Tasks, advertisingTask),
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

func advertisingTask(task string) bool {
	return strings.EqualFold(task, "ADVERTISE") || strings.EqualFold(task, "MANAGE")
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
