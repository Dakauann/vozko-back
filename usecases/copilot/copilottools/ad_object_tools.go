package copilottools

import (
	"context"
	"fmt"
	"sort"

	"vozko/domain/advertising"
	"vozko/domain/copilot"
	"vozko/domain/tools"
	"vozko/domain/workspace"
	adsuc "vozko/usecases/advertising"
)

type AdLive interface {
	Insights(ctx context.Context, q advertising.LiveQuery) (*adsuc.LiveReport, error)
}

var levelNames = map[advertising.Level]string{
	advertising.LevelCampaign: "campanha",
	advertising.LevelAdSet:    "conjunto",
	advertising.LevelAd:       "anúncio",
}

func adsRange(since, until string) (advertising.DateRange, error) {
	if since == "" && until == "" {
		return advertising.DateRange{}, nil
	}
	r, err := advertising.NewDateRange(since, until)
	if err != nil {
		return advertising.DateRange{}, fmt.Errorf("%w: período inválido; use YYYY-MM-DD em since e until", errInvalidArgs)
	}
	return r, nil
}

type duplicateAdArgs struct {
	MetaID     string `json:"meta_id" req:"true" desc:"meta_id exato de ads_results do item a duplicar"`
	ParentID   string `json:"parent_id" desc:"meta_id de destino: a campanha para um conjunto ou o conjunto para um anúncio; vazio mantém o mesmo"`
	DeepCopy   bool   `json:"deep_copy" desc:"true copia também o que está dentro (conjuntos e anúncios); vale para campanha e conjunto"`
	NameSuffix string `json:"name_suffix" desc:"texto somado ao nome da cópia, ex.: ' - cópia'"`
}

func (a duplicateAdArgs) request() (string, advertising.CopyRequest, error) {
	id, err := metaID(a.MetaID)
	if err != nil {
		return "", advertising.CopyRequest{}, err
	}
	req := advertising.CopyRequest{DeepCopy: a.DeepCopy, NameSuffix: a.NameSuffix}
	if a.ParentID != "" {
		if req.ParentID, err = metaID(a.ParentID); err != nil {
			return "", advertising.CopyRequest{}, err
		}
	}
	return id, req, nil
}

type duplicateAdTool struct{ deps AdsDeps }

func NewDuplicateAdTool(deps AdsDeps) copilot.Tool { return &duplicateAdTool{deps: deps} }

func (t *duplicateAdTool) Meta() copilot.Meta { return adsMeta(workspace.ActionCreate, true) }

func (t *duplicateAdTool) Definition() tools.Definition {
	return definition("duplicate_ad",
		"Duplica uma campanha, conjunto ou anúncio na Meta, opcionalmente em outra campanha ou conjunto da mesma conta. "+
			"A cópia nasce desligada. Só depois da aprovação do usuário.",
		duplicateAdArgs{})
}

func (t *duplicateAdTool) check(ctx context.Context, cc copilot.Context, args map[string]interface{}) (*advertising.Object, string, advertising.CopyRequest, error) {
	a, err := validateArgs[duplicateAdArgs](nil, cc, args)
	if err != nil {
		return nil, "", advertising.CopyRequest{}, err
	}
	id, req, err := a.request()
	if err != nil {
		return nil, "", advertising.CopyRequest{}, err
	}
	object, err := t.deps.Manage.CheckCopy(ctx, cc.WorkspaceID, id, req)
	return object, id, req, err
}

func (t *duplicateAdTool) Validate(ctx context.Context, cc copilot.Context, args map[string]interface{}) error {
	_, _, _, err := t.check(ctx, cc, args)
	return adsValidation("duplicate_ad", err)
}

func (t *duplicateAdTool) Describe(ctx context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	object, _, req, err := t.check(ctx, cc, args)
	if err != nil {
		return []copilot.Field{{Key: "item", Value: "item desconhecido"}}
	}
	fields := []copilot.Field{{Key: "item", Value: object.Name}, {Key: "level", Value: levelNames[object.Level]}}
	if req.ParentID != "" {
		fields = append(fields, copilot.Field{Key: "target", Value: req.ParentID})
	}
	if object.Level != advertising.LevelAd {
		copies := "só o item"
		if req.DeepCopy {
			copies = "o item e tudo dentro dele"
		}
		fields = append(fields, copilot.Field{Key: "copies", Value: copies})
	}
	return fields
}

func (t *duplicateAdTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a duplicateAdArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	id, req, err := a.request()
	if err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	copyID, err := t.deps.Manage.Copy(ctx, cc.WorkspaceID, id, req)
	if err != nil {
		return adsFailure("duplicate_ad", err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"copy_meta_id": copyID, "on": false}}
}

type adLifecycleTool struct {
	deps   AdsDeps
	action advertising.Lifecycle
}

func NewArchiveAdTool(deps AdsDeps) copilot.Tool {
	return &adLifecycleTool{deps: deps, action: advertising.LifecycleArchive}
}

func NewDeleteAdTool(deps AdsDeps) copilot.Tool {
	return &adLifecycleTool{deps: deps, action: advertising.LifecycleDelete}
}

func (t *adLifecycleTool) Meta() copilot.Meta {
	if t.action == advertising.LifecycleDelete {
		return adsMeta(workspace.ActionDelete, true)
	}
	return adsMeta(workspace.ActionUpdate, true)
}

func (t *adLifecycleTool) Definition() tools.Definition {
	if t.action == advertising.LifecycleDelete {
		return definition("delete_ad",
			"Exclui uma campanha, conjunto ou anúncio na Meta. Não dá para desfazer e os resultados deixam de aparecer na Meta. "+
				"Prefira archive_ad quando a pessoa só quer tirar da lista. Só depois da aprovação do usuário.",
			adStatusArgs{})
	}
	return definition("archive_ad",
		"Arquiva uma campanha, conjunto ou anúncio na Meta: para de veicular e sai da lista, mas os resultados continuam. Só depois da aprovação do usuário.",
		adStatusArgs{})
}

func (t *adLifecycleTool) check(ctx context.Context, cc copilot.Context, args map[string]interface{}) (*advertising.Object, error) {
	a, err := validateArgs[adStatusArgs](nil, cc, args)
	if err != nil {
		return nil, err
	}
	id, err := metaID(a.MetaID)
	if err != nil {
		return nil, err
	}
	return t.deps.Manage.CheckLifecycle(ctx, cc.WorkspaceID, id, t.action)
}

func (t *adLifecycleTool) Validate(ctx context.Context, cc copilot.Context, args map[string]interface{}) error {
	_, err := t.check(ctx, cc, args)
	return adsValidation(t.Definition().Name, err)
}

func (t *adLifecycleTool) Describe(ctx context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	object, err := t.check(ctx, cc, args)
	if err != nil {
		return []copilot.Field{{Key: "item", Value: "item desconhecido"}}
	}
	change := "arquivar"
	if t.action == advertising.LifecycleDelete {
		change = "excluir de vez, sem como desfazer"
	}
	return []copilot.Field{{Key: "item", Value: object.Name}, {Key: "level", Value: levelNames[object.Level]}, {Key: "change", Value: change}}
}

func (t *adLifecycleTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a adStatusArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	id, err := metaID(a.MetaID)
	if err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	object, err := t.deps.Manage.Lifecycle(ctx, cc.WorkspaceID, id, t.action)
	if err != nil {
		return adsFailure(t.Definition().Name, err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"meta_id": object.MetaID, "status": string(object.Status)}}
}

type adsBreakdownArgs struct {
	AdAccountID string   `json:"ad_account_id" req:"true" id:"true" desc:"ad_account_id de list_ad_accounts"`
	Level       string   `json:"level" enum:"campaign,adset,ad" desc:"campaign (padrão), adset ou ad"`
	Breakdowns  []string `json:"breakdowns" desc:"age, gender, country, region, publisher_platform, platform_position, device_platform ou hourly_stats_aggregated_by_advertiser_time_zone; só combinações aceitas pela Meta, como age+gender ou publisher_platform+platform_position"`
	ObjectIDs   []string `json:"object_ids" desc:"meta_id de ads_results para limitar a itens específicos"`
	Window      string   `json:"window" enum:"1d_view,1d_click,7d_click,28d_click" desc:"janela de atribuição; vazio usa a padrão da conta"`
	Since       string   `json:"since" desc:"primeiro dia YYYY-MM-DD; sem período, os últimos 30 dias"`
	Until       string   `json:"until" desc:"último dia YYYY-MM-DD"`
}

type adsBreakdownTool struct{ deps AdsDeps }

func NewAdsBreakdownTool(deps AdsDeps) copilot.Tool { return &adsBreakdownTool{deps: deps} }

func (t *adsBreakdownTool) Meta() copilot.Meta { return adsMeta(workspace.ActionRead, false) }

func (t *adsBreakdownTool) Definition() tools.Definition {
	return definition("ads_breakdown",
		"Resultados ao vivo da Meta por idade, gênero, país, região, plataforma, posicionamento, dispositivo ou hora do dia, "+
			"com alcance, frequência e métricas de vídeo. Use para responder onde e para quem o anúncio funciona melhor. "+
			"Valores em unidades da moeda da conta.",
		adsBreakdownArgs{})
}

func (t *adsBreakdownTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a adsBreakdownArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	account, err := t.deps.account(ctx, cc, a.AdAccountID)
	if err != nil {
		return adsFailure("ads_breakdown", err)
	}
	dates, err := adsRange(a.Since, a.Until)
	if err != nil {
		return adsFailure("ads_breakdown", err)
	}
	q := advertising.LiveQuery{
		WorkspaceID: cc.WorkspaceID, AccountID: account.ID, Level: advertising.Level(a.Level), ObjectIDs: a.ObjectIDs, Range: dates,
		Breakdowns: advertising.Typed[advertising.Breakdown](a.Breakdowns),
	}
	if a.Window != "" {
		q.Windows = []advertising.AttributionWindow{advertising.AttributionWindow(a.Window)}
	}
	report, err := t.deps.Live.Insights(ctx, q)
	if err != nil {
		return adsFailure("ads_breakdown", err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: breakdownData(report)}
}

func breakdownData(r *adsuc.LiveReport) map[string]interface{} {
	rows := append([]advertising.LiveRow(nil), r.Rows...)
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Values.SpendMicros > rows[j].Values.SpendMicros })
	shown := rows
	if len(shown) > maxAdRowsShown {
		shown = shown[:maxAdRowsShown]
	}
	items := make([]map[string]interface{}, 0, len(shown))
	for _, row := range shown {
		v := row.Values
		item := map[string]interface{}{
			"meta_id": row.ObjectID, "spend": advertising.MicrosToAmount(v.SpendMicros), "impressions": v.Impressions,
			"reach": v.Reach, "frequency": v.Frequency, "link_clicks": v.LinkClicks, "ctr": v.CTR(),
			"results": v.Results, "result_type": v.ResultAction, "cost_per_result": optionalAmount(v.CostPerResult()),
		}
		if len(row.Dimensions) > 0 {
			item["segment"] = row.Dimensions
		}
		if v.Video.Plays > 0 {
			item["video_plays"], item["thruplays"], item["cost_per_thruplay"] = v.Video.Plays, v.Video.ThruPlays, optionalAmount(v.CostPerThruPlay())
		}
		items = append(items, item)
	}
	return map[string]interface{}{
		"account": r.Account.Name, "currency": r.Account.Currency,
		"since": r.Query.Range.Since.Format(advertising.DayLayout), "until": r.Query.Range.Until.Format(advertising.DayLayout),
		"rows": items, "rows_total": len(r.Rows),
	}
}
