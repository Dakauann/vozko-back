package copilottools

import (
	"context"
	"strings"

	"vozko/domain/advertising"
	"vozko/domain/copilot"
	"vozko/domain/tools"
	"vozko/domain/workspace"
)

const maxPostTextRunes = 200

type AdCreativeSources interface {
	Posts(ctx context.Context, workspaceID, accountID, pageID, platform string) ([]advertising.RemotePost, error)
	Apps(ctx context.Context, workspaceID, accountID string) ([]advertising.RemoteApp, error)
	Catalogs(ctx context.Context, workspaceID, accountID string) ([]advertising.RemoteCatalog, error)
}

type listPagePostsArgs struct {
	AdAccountID string `json:"ad_account_id" req:"true" desc:"ad_account_id de list_ad_accounts (também aceita o id da Meta ou o nome exato da conta)"`
	PageID      string `json:"page_id" req:"true" desc:"page_id exato de list_ad_pages"`
	Platform    string `json:"platform" req:"true" enum:"facebook,instagram" desc:"facebook para publicações da página, instagram para as do Instagram ligado a ela"`
}

type listPagePostsTool struct{ deps AdsDeps }

func NewListPagePostsTool(deps AdsDeps) copilot.Tool { return &listPagePostsTool{deps: deps} }

func (t *listPagePostsTool) Meta() copilot.Meta { return adsMeta(workspace.ActionCreate, false) }

func (t *listPagePostsTool) Definition() tools.Definition {
	return definition("list_page_posts",
		"Lista as publicações recentes de uma página do Facebook ou do Instagram ligado a ela, para anunciar uma publicação existente "+
			"(format EXISTING_POST com post_id e post_platform). Para o Instagram, passe também instagram_user_id da página no anúncio.",
		listPagePostsArgs{})
}

func (t *listPagePostsTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a listPagePostsArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	account, err := t.deps.account(ctx, cc, a.AdAccountID)
	if err != nil {
		return adsFailure("list_page_posts", err)
	}
	posts, err := t.deps.Sources.Posts(ctx, cc.WorkspaceID, account.ID, strings.TrimSpace(a.PageID), a.Platform)
	if err != nil {
		return adsFailure("list_page_posts", err)
	}
	out := make([]map[string]interface{}, 0, len(posts))
	for _, p := range posts {
		row := map[string]interface{}{"post_id": p.ID, "post_platform": p.Platform, "text": clipText(p.Message, maxPostTextRunes), "has_picture": p.PictureURL != ""}
		if p.CreatedTime != nil {
			row["created_at"] = p.CreatedTime.Format("2006-01-02 15:04")
		}
		out = append(out, row)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"posts": out}}
}

type listAdAppsTool struct{ deps AdsDeps }

func NewListAdAppsTool(deps AdsDeps) copilot.Tool { return &listAdAppsTool{deps: deps} }

func (t *listAdAppsTool) Meta() copilot.Meta { return adsMeta(workspace.ActionCreate, false) }

func (t *listAdAppsTool) Definition() tools.Definition {
	return definition("list_ad_apps",
		"Lista os apps que a conta de anúncios pode promover, com os links de loja de cada um. Use app_id e um dos store_urls em anúncios "+
			"com objective OUTCOME_APP_PROMOTION e destination APP. Sem app na lista, o app precisa ser registrado na Meta antes.",
		adAccountArgs{})
}

func (t *listAdAppsTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a adAccountArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	account, err := t.deps.account(ctx, cc, a.AdAccountID)
	if err != nil {
		return adsFailure("list_ad_apps", err)
	}
	apps, err := t.deps.Sources.Apps(ctx, cc.WorkspaceID, account.ID)
	if err != nil {
		return adsFailure("list_ad_apps", err)
	}
	out := make([]map[string]interface{}, 0, len(apps))
	for _, app := range apps {
		out = append(out, map[string]interface{}{"app_id": app.ID, "name": app.Name, "store_urls": app.StoreURLs})
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"apps": out}}
}

type listAdCatalogsTool struct{ deps AdsDeps }

func NewListAdCatalogsTool(deps AdsDeps) copilot.Tool { return &listAdCatalogsTool{deps: deps} }

func (t *listAdCatalogsTool) Meta() copilot.Meta { return adsMeta(workspace.ActionCreate, false) }

func (t *listAdCatalogsTool) Definition() tools.Definition {
	return definition("list_ad_catalogs",
		"Lista os catálogos de produtos do portfólio empresarial da conta e os conjuntos de produtos de cada um. Use catalog_id e product_set_id "+
			"em anúncios com objective OUTCOME_SALES, destination CATALOG e format CATALOG. Sem catálogo, o usuário cria um no Commerce Manager da Meta.",
		adAccountArgs{})
}

func (t *listAdCatalogsTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a adAccountArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	account, err := t.deps.account(ctx, cc, a.AdAccountID)
	if err != nil {
		return adsFailure("list_ad_catalogs", err)
	}
	catalogs, err := t.deps.Sources.Catalogs(ctx, cc.WorkspaceID, account.ID)
	if err != nil {
		return adsFailure("list_ad_catalogs", err)
	}
	out := make([]map[string]interface{}, 0, len(catalogs))
	for _, c := range catalogs {
		sets := make([]map[string]interface{}, 0, len(c.ProductSets))
		for _, s := range c.ProductSets {
			sets = append(sets, map[string]interface{}{"product_set_id": s.ID, "name": s.Name, "products": s.ProductCount})
		}
		out = append(out, map[string]interface{}{"catalog_id": c.ID, "name": c.Name, "product_sets": sets})
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"catalogs": out}}
}
