package copilottools

import (
	"context"

	"vozko/domain/advertising"
	"vozko/domain/copilot"
	"vozko/domain/tools"
	"vozko/domain/workspace"
)

type getAdCreativeArgs struct {
	MetaID string `json:"meta_id" req:"true" desc:"meta_id exato de um anúncio em ads_results"`
}

type getAdCreativeTool struct{ deps AdsDeps }

func NewGetAdCreativeTool(deps AdsDeps) copilot.Tool { return &getAdCreativeTool{deps: deps} }

func (t *getAdCreativeTool) Meta() copilot.Meta { return adsMeta(workspace.ActionRead, false) }

func (t *getAdCreativeTool) Definition() tools.Definition {
	return definition("get_ad_creative",
		"Mostra do que um anúncio publicado é feito: formato, textos, títulos de cada cartão, link, botão, mensagem pronta, formulário, "+
			"melhorias Advantage+ e as imagens e vídeos (media_id meta:, com o link de prévia). Use antes de editar textos e para reaproveitar "+
			"as mesmas imagens num anúncio novo com save_ad_draft ou create_ad, passando source_ad_id e os media_id meta: daqui.",
		getAdCreativeArgs{})
}

func (t *getAdCreativeTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a getAdCreativeArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	id, err := metaID(a.MetaID)
	if err != nil {
		return adsFailure("get_ad_creative", err)
	}
	detail, err := t.deps.Editor.Detail(ctx, cc.WorkspaceID, id)
	if err != nil {
		return adsFailure("get_ad_creative", err)
	}
	if detail.Object.Level != advertising.LevelAd || detail.Creative == nil {
		return adsFailure("get_ad_creative", errSourceAd)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{
		"meta_id":       id,
		"name":          detail.Object.Name,
		"ad_account_id": detail.Object.AdAccountID,
		"creative":      detail.Creative,
		"media_urls":    detail.MediaURLs,
	}}
}
