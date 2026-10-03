package copilottools

import (
	"context"

	"vozko/domain/copilot"
	"vozko/domain/tools"
	"vozko/domain/workspace"
)

type connectAdAccountTool struct{}

func NewConnectAdAccountTool() copilot.Tool { return connectAdAccountTool{} }

func (connectAdAccountTool) Meta() copilot.Meta { return adsMeta(workspace.ActionCreate, false) }

func (connectAdAccountTool) Definition() tools.Definition {
	return definition("connect_ad_account",
		"Mostra na conversa o botão que conecta contas de anúncios da Meta. O usuário entra com o Facebook numa janela da Meta e escolhe "+
			"as contas; você nunca pede senha nem código. Use quando list_ad_accounts não trouxer contas ou a conta pedida não estiver lá. "+
			"Depois de chamar, diga em uma frase o que o botão faz e chame list_ad_accounts quando o usuário disser que terminou.",
		struct{}{})
}

func (connectAdAccountTool) Execute(context.Context, copilot.Context, map[string]interface{}) copilot.Result {
	return copilot.Result{Status: copilot.StatusOK, Card: copilot.NewConnectAdAccountCard(), Data: map[string]interface{}{"card_shown": "connect_ad_account"}}
}
