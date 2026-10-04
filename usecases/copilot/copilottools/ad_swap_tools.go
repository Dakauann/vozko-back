package copilottools

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"vozko/domain/advertising"
	"vozko/domain/copilot"
	"vozko/domain/tools"
	"vozko/domain/workspace"
)

type swapAdCreativeArgs struct {
	MetaID string `json:"meta_id" req:"true" desc:"meta_id exato de um anúncio em ads_results"`
	adCreativeArgs
}

type swapAdCreativeTool struct{ deps adManage }

func (t *swapAdCreativeTool) Meta() copilot.Meta { return adsMeta(workspace.ActionUpdate, true) }

func (t *swapAdCreativeTool) Definition() tools.Definition {
	return adDraftDefinition("swap_ad_creative",
		"Troca o criativo de um anúncio publicado: formato, imagem ou vídeo, cartões do carrossel, textos, mensagem pronta, perguntas prontas e melhorias Advantage+. "+
			"Mande só o que muda; o resto continua como está no anúncio. Cartões e listas são substituídos inteiros, então repita os que ficam "+
			"(os media_id atuais aparecem como meta:...). A página, o conjunto e o destino continuam os mesmos. A Meta analisa o anúncio de novo. "+
			"Só depois da aprovação do usuário, que vê a prévia de todos os posicionamentos.",
		swapAdCreativeArgs{})
}

type swapPlan struct {
	id       string
	detail   *advertising.ObjectDetail
	creative advertising.CreativeDraft
	checked  *advertising.ObjectDetail
	changed  []string
}

func (t *swapAdCreativeTool) plan(ctx context.Context, cc copilot.Context, args map[string]interface{}) (*swapPlan, error) {
	var a struct {
		MetaID string `json:"meta_id" req:"true"`
	}
	if err := decodeArgs(args, &a); err != nil {
		return nil, err
	}
	id, err := metaID(a.MetaID)
	if err != nil {
		return nil, err
	}
	detail, err := t.deps.ads.Editor.Detail(ctx, cc.WorkspaceID, id)
	if err != nil {
		return nil, err
	}
	if detail.Object == nil || detail.Object.Level != advertising.LevelAd {
		return nil, fmt.Errorf("%w: meta_id %s não é um anúncio; o criativo só existe no nível do anúncio", errInvalidArgs, id)
	}
	if detail.Creative == nil {
		return nil, fmt.Errorf("%w: a Meta não devolveu o criativo deste anúncio; troque pela tela do editor", errInvalidArgs)
	}
	merged, changed, err := overlayCreative(*detail.Creative, args)
	if err != nil {
		return nil, err
	}
	creative, err := merged.creative(*detail.Creative)
	if err != nil {
		return nil, err
	}
	checked, err := t.deps.ads.Editor.CheckEdit(ctx, cc.WorkspaceID, id, advertising.ObjectEdit{Creative: &creative})
	if err != nil {
		return nil, err
	}
	return &swapPlan{id: id, detail: detail, creative: creative, checked: checked, changed: changed}, nil
}

func overlayCreative(current advertising.CreativeDraft, args map[string]interface{}) (adCreativeArgs, []string, error) {
	var base adCreativeArgs
	base.setCreative(current)
	fields := compactArgs(base)
	var changed []string
	for _, key := range creativeArgKeys {
		if value, ok := args[key]; ok {
			fields[key] = value
			changed = append(changed, key)
		}
	}
	if len(changed) == 0 {
		return adCreativeArgs{}, nil, fmt.Errorf("%w: diga o que muda no criativo", errInvalidArgs)
	}
	var merged adCreativeArgs
	bindArgs(fields, &merged)
	return merged, changed, nil
}

func (t *swapAdCreativeTool) Validate(ctx context.Context, cc copilot.Context, args map[string]interface{}) error {
	_, err := t.plan(ctx, cc, args)
	return adsValidation("swap_ad_creative", err)
}

func (t *swapAdCreativeTool) Describe(ctx context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	p, err := t.plan(ctx, cc, args)
	if err != nil {
		return []copilot.Field{{Key: "item", Value: "anúncio desconhecido"}}
	}
	fields := []copilot.Field{
		{Key: "item", Value: p.detail.Object.Name},
		{Key: "format", Value: string(p.creative.Format)},
		{Key: "changes", Value: strings.Join(p.changed, ", ")},
		{Key: "enhancements", Value: enhancementsText(p.creative.Enhancements)},
	}
	if slices.Contains(p.changed, "format") && p.detail.Creative.Format != p.creative.Format {
		fields = append(fields, copilot.Field{Key: "from", Value: string(p.detail.Creative.Format)})
	}
	return fields
}

func enhancementsText(on bool) string {
	if on {
		return "ligadas: a Meta pode cortar e ajustar"
	}
	return "desligadas: o anúncio vai exatamente como foi montado"
}

func (t *swapAdCreativeTool) Preview(ctx context.Context, cc copilot.Context, args map[string]interface{}) *copilot.Preview {
	p, err := t.plan(ctx, cc, args)
	if err != nil {
		return nil
	}
	destination := advertising.Destination(p.detail.Object.DestinationType)
	preview := creativeFields(p.creative, destination, p.checked.MediaURLs)
	if account, err := t.deps.ads.account(ctx, cc, p.detail.Object.AdAccountID); err == nil {
		preview.AccountName, preview.Currency = account.Name, account.Currency
	}
	if pages, err := t.deps.ads.Assets.Pages(ctx, cc.WorkspaceID, p.detail.Object.AdAccountID); err == nil {
		for _, page := range pages {
			if page.Page.PageID == p.detail.Identity.PageID {
				preview.PageName, preview.PagePictureURL = page.Page.Name, page.Page.PictureURL
			}
		}
	}
	return &copilot.Preview{Kind: PreviewAdCreative, Data: preview}
}

func (t *swapAdCreativeTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	p, err := t.plan(ctx, cc, args)
	if err != nil {
		return adsFailure("swap_ad_creative", err)
	}
	object, err := t.deps.ads.Editor.Edit(ctx, cc.WorkspaceID, p.id, advertising.ObjectEdit{Creative: &p.creative})
	if err != nil {
		return adsFailure("swap_ad_creative", err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"meta_id": object.MetaID, "name": object.Name, "review": "a Meta analisa o anúncio de novo"}}
}
