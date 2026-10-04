package copilottools

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"vozko/domain/advertising"
	"vozko/domain/copilot"
	"vozko/domain/tools"
	"vozko/domain/workspace"
	adsuc "vozko/usecases/advertising"
)

func itemRefused(tool, id string, err error) error {
	if errors.Is(err, errInvalidArgs) {
		return err
	}
	return fmt.Errorf("%w: o item %s não pode mudar: %s", errInvalidArgs, id, adsFailure(tool, err).Message)
}

func checkedItems(tool string, results []adsuc.BulkResult, err error) ([]*advertising.Object, error) {
	if err != nil {
		return nil, err
	}
	objects := make([]*advertising.Object, 0, len(results))
	for _, r := range results {
		if r.Err != nil {
			return nil, itemRefused(tool, r.MetaID, r.Err)
		}
		objects = append(objects, r.Object)
	}
	return objects, nil
}

func itemsText(count int) string {
	if count == 1 {
		return "1 item"
	}
	return strconv.Itoa(count) + " itens"
}

func bulkResult(tool string, results []adsuc.BulkResult) copilot.Result {
	items := make([]map[string]interface{}, 0, len(results))
	changed, firstError := 0, ""
	for _, r := range results {
		item := map[string]interface{}{"meta_id": r.MetaID, "ok": r.Err == nil}
		if r.Object != nil {
			item["name"] = r.Object.Name
		}
		if r.Err != nil {
			message := adsFailure(tool, r.Err).Message
			item["error"] = message
			if firstError == "" {
				firstError = message
			}
		} else {
			changed++
		}
		items = append(items, item)
	}
	if changed == 0 {
		return copilot.Result{Status: copilot.StatusError, Message: "nenhum item mudou: " + firstError}
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{
		"changed": changed, "failed": len(results) - changed, "items": items,
	}}
}

type bulkTargetsArgs struct {
	MetaIDs []string `json:"meta_ids" req:"true" desc:"até 50 meta_id exatos de ads_results (campanhas, conjuntos ou anúncios)"`
}

type bulkStatusTool struct {
	deps adManage
	on   bool
}

func (t *bulkStatusTool) Meta() copilot.Meta {
	if t.on {
		return adsMeta(workspace.ActionStart, true)
	}
	return adsMeta(workspace.ActionStop, true)
}

func (t *bulkStatusTool) Definition() tools.Definition {
	if t.on {
		return definition("bulk_turn_on_ads",
			"Liga de uma vez até 50 campanhas, conjuntos ou anúncios na Meta, o que volta a gastar os orçamentos. Só depois da aprovação do usuário.",
			bulkTargetsArgs{})
	}
	return definition("bulk_turn_off_ads",
		"Desliga de uma vez até 50 campanhas, conjuntos ou anúncios na Meta. Só depois da aprovação do usuário.",
		bulkTargetsArgs{})
}

func (t *bulkStatusTool) check(ctx context.Context, cc copilot.Context, args map[string]interface{}) ([]*advertising.Object, error) {
	a, err := validateArgs[bulkTargetsArgs](nil, cc, args)
	if err != nil {
		return nil, err
	}
	ids, err := metaIDs(a.MetaIDs)
	if err != nil {
		return nil, err
	}
	results, err := t.deps.Bulk.CheckStatus(ctx, cc.WorkspaceID, ids, t.on)
	return checkedItems(t.Definition().Name, results, err)
}

func (t *bulkStatusTool) Validate(ctx context.Context, cc copilot.Context, args map[string]interface{}) error {
	_, err := t.check(ctx, cc, args)
	return adsValidation(t.Definition().Name, err)
}

func (t *bulkStatusTool) Describe(ctx context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	objects, err := t.check(ctx, cc, args)
	if err != nil {
		return []copilot.Field{{Key: "items", Value: "itens desconhecidos"}}
	}
	names := make([]string, 0, len(objects))
	for _, o := range objects {
		names = append(names, o.Name+" ("+levelNames[o.Level]+")")
	}
	change := "desligar"
	if t.on {
		change = "ligar"
	}
	return []copilot.Field{{Key: "change", Value: change}, {Key: "items", Value: itemsText(len(objects)) + ": " + strings.Join(names, ", ")}}
}

func (t *bulkStatusTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a bulkTargetsArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	ids, err := metaIDs(a.MetaIDs)
	if err != nil {
		return adsFailure(t.Definition().Name, err)
	}
	results, err := t.deps.Bulk.SetStatus(ctx, cc.WorkspaceID, ids, t.on)
	if err != nil {
		return adsFailure(t.Definition().Name, err)
	}
	return bulkResult(t.Definition().Name, results)
}

type bulkEditTextArgs struct {
	MetaIDs   []string `json:"meta_ids" req:"true" desc:"até 50 meta_id exatos de ads_results: anúncios; para name também campanhas e conjuntos"`
	Field     string   `json:"field" req:"true" enum:"name,primaryText,headline,description,link" desc:"o que mudar"`
	Mode      string   `json:"mode" req:"true" enum:"set,replace" desc:"set troca o texto inteiro por value; replace troca o trecho find por replace dentro do texto atual de cada item"`
	Value     string   `json:"value" desc:"o novo texto, com mode set; vazio limpa título, descrição ou link"`
	Find      string   `json:"find" desc:"o trecho a procurar, com mode replace"`
	Replace   string   `json:"replace" desc:"o trecho que entra no lugar, com mode replace; vazio apaga o trecho"`
	MatchCase bool     `json:"match_case" desc:"true diferencia maiúsculas de minúsculas ao procurar"`
}

func (a bulkEditTextArgs) change() advertising.BulkChange {
	return advertising.BulkChange{
		Field: advertising.BulkField(a.Field), Mode: advertising.BulkMode(a.Mode),
		Value: a.Value, Find: a.Find, Replace: a.Replace, MatchCase: a.MatchCase,
	}
}

var bulkFieldNames = map[advertising.BulkField]string{
	advertising.BulkName:        "nome",
	advertising.BulkPrimaryText: "texto principal",
	advertising.BulkHeadline:    "título",
	advertising.BulkDescription: "descrição",
	advertising.BulkLink:        "link",
}

func bulkChangeText(c advertising.BulkChange) string {
	if c.Mode == advertising.BulkReplace {
		return "trocar \"" + c.Find + "\" por \"" + c.Replace + "\""
	}
	if strings.TrimSpace(c.Value) == "" {
		return "deixar vazio"
	}
	return "passa a ser \"" + c.Value + "\""
}

type bulkEditTextTool struct{ deps adManage }

func (t *bulkEditTextTool) Meta() copilot.Meta { return adsMeta(workspace.ActionUpdate, true) }

func (t *bulkEditTextTool) Definition() tools.Definition {
	return definition("bulk_edit_ads_text",
		"Muda de uma vez o nome, o texto principal, o título, a descrição ou o link de até 50 itens publicados: troca o texto inteiro (set) "+
			"ou só um trecho dentro de cada um (replace). Texto novo passa de novo pela revisão da Meta. Só depois da aprovação do usuário.",
		bulkEditTextArgs{})
}

func (t *bulkEditTextTool) request(cc copilot.Context, args map[string]interface{}) ([]string, advertising.BulkChange, error) {
	a, err := validateArgs[bulkEditTextArgs](nil, cc, args)
	if err != nil {
		return nil, advertising.BulkChange{}, err
	}
	ids, err := metaIDs(a.MetaIDs)
	if err != nil {
		return nil, advertising.BulkChange{}, err
	}
	return ids, a.change(), nil
}

func (t *bulkEditTextTool) Validate(ctx context.Context, cc copilot.Context, args map[string]interface{}) error {
	ids, change, err := t.request(cc, args)
	if err != nil {
		return adsValidation("bulk_edit_ads_text", err)
	}
	results, err := t.deps.Bulk.CheckEdit(ctx, cc.WorkspaceID, ids, change)
	if err != nil {
		return adsValidation("bulk_edit_ads_text", err)
	}
	unchanged := 0
	for _, r := range results {
		if errors.Is(r.Err, advertising.ErrNothingToChange) {
			unchanged++
			continue
		}
		if r.Err != nil {
			return itemRefused("bulk_edit_ads_text", r.MetaID, r.Err)
		}
	}
	if unchanged == len(ids) {
		return fmt.Errorf("%w: nenhum item muda com essa troca; confira o texto atual em ads_results", errInvalidArgs)
	}
	return nil
}

func (t *bulkEditTextTool) Describe(_ context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	ids, change, err := t.request(cc, args)
	if err != nil {
		return []copilot.Field{{Key: "items", Value: "itens desconhecidos"}}
	}
	return []copilot.Field{
		{Key: "items", Value: itemsText(len(ids))},
		{Key: "field", Value: bulkFieldNames[change.Field]},
		{Key: "change", Value: bulkChangeText(change)},
	}
}

func (t *bulkEditTextTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	ids, change, err := t.request(cc, args)
	if err != nil {
		return adsFailure("bulk_edit_ads_text", err)
	}
	results, err := t.deps.Bulk.Edit(ctx, cc.WorkspaceID, ids, change)
	if err != nil {
		return adsFailure("bulk_edit_ads_text", err)
	}
	return bulkResult("bulk_edit_ads_text", results)
}

type bulkChangeArgs struct {
	AdAccountID string   `json:"ad_account_id" req:"true" desc:"ad_account_id de list_ad_accounts (também aceita o id da Meta ou o nome exato da conta), a conta de todos os itens"`
	MetaIDs     []string `json:"meta_ids" req:"true" desc:"até 50 meta_id exatos de ads_results, de campanhas ou conjuntos dessa conta"`
	EndDate     string   `json:"end_date" desc:"novo último dia de veiculação YYYY-MM-DD no fuso da conta"`
	Budget      float64  `json:"budget" desc:"novo valor de orçamento de cada item na moeda da conta, no tipo atual de cada um (diário ou total); 50 significa 50 reais em uma conta BRL"`
}

func (a bulkChangeArgs) edit(account *advertising.AdAccount) (advertising.ObjectEdit, error) {
	endAt, err := adDraftArgs{EndDate: a.EndDate}.endAt(account)
	if err != nil {
		return advertising.ObjectEdit{}, err
	}
	edit := advertising.ObjectEdit{EndAt: endAt}
	if a.Budget != 0 {
		amount, err := advertising.AmountToMinor(account.Currency, a.Budget)
		if err != nil {
			return advertising.ObjectEdit{}, fmt.Errorf("%w: budget deve ser maior que zero", errInvalidArgs)
		}
		edit.Budget = &advertising.Budget{Amount: amount}
	}
	if edit.Empty() {
		return advertising.ObjectEdit{}, fmt.Errorf("%w: diga end_date ou budget", errInvalidArgs)
	}
	return edit, nil
}

type bulkChangePlan struct {
	account *advertising.AdAccount
	ids     []string
	edit    advertising.ObjectEdit
}

type bulkChangeTool struct{ deps adManage }

func (t *bulkChangeTool) Meta() copilot.Meta { return adsMeta(workspace.ActionUpdate, true) }

func (t *bulkChangeTool) Definition() tools.Definition {
	return definition("bulk_change_ads",
		"Muda de uma vez a data de término ou o valor do orçamento de até 50 campanhas ou conjuntos da mesma conta. "+
			"O orçamento mantém o tipo de cada item (diário ou total) e a Meta aceita até 4 mudanças de orçamento por hora em cada um. "+
			"Só depois da aprovação do usuário.",
		bulkChangeArgs{})
}

func (t *bulkChangeTool) plan(ctx context.Context, cc copilot.Context, args map[string]interface{}) (*bulkChangePlan, error) {
	a, err := validateArgs[bulkChangeArgs](nil, cc, args)
	if err != nil {
		return nil, err
	}
	account, err := t.deps.ads.account(ctx, cc, a.AdAccountID)
	if err != nil {
		return nil, err
	}
	ids, err := metaIDs(a.MetaIDs)
	if err != nil {
		return nil, err
	}
	edit, err := a.edit(account)
	if err != nil {
		return nil, err
	}
	results, err := t.deps.Bulk.CheckApply(ctx, cc.WorkspaceID, ids, edit)
	objects, err := checkedItems("bulk_change_ads", results, err)
	if err != nil {
		return nil, err
	}
	for _, o := range objects {
		if o.AdAccountID != account.ID {
			return nil, fmt.Errorf("%w: o item %s é de outra conta de anúncios; mande só itens de %s", errInvalidArgs, o.MetaID, account.Name)
		}
	}
	return &bulkChangePlan{account: account, ids: ids, edit: edit}, nil
}

func (t *bulkChangeTool) Validate(ctx context.Context, cc copilot.Context, args map[string]interface{}) error {
	_, err := t.plan(ctx, cc, args)
	return adsValidation("bulk_change_ads", err)
}

func (t *bulkChangeTool) Describe(ctx context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	p, err := t.plan(ctx, cc, args)
	if err != nil {
		return []copilot.Field{{Key: "items", Value: "itens desconhecidos"}}
	}
	fields := []copilot.Field{{Key: "account", Value: p.account.Name}, {Key: "items", Value: itemsText(len(p.ids))}}
	if p.edit.EndAt != nil {
		fields = append(fields, copilot.Field{Key: "ends", Value: lastDayText(p.edit.EndAt, p.account)})
	}
	if p.edit.Budget != nil {
		fields = append(fields, copilot.Field{Key: "budget", Value: minorText(p.account.Currency, p.edit.Budget.Amount) + " em cada item, no tipo atual de cada um (diário ou total)"})
	}
	return fields
}

func (t *bulkChangeTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	p, err := t.plan(ctx, cc, args)
	if err != nil {
		return adsFailure("bulk_change_ads", err)
	}
	results, err := t.deps.Bulk.Apply(ctx, cc.WorkspaceID, p.ids, p.edit)
	if err != nil {
		return adsFailure("bulk_change_ads", err)
	}
	return bulkResult("bulk_change_ads", results)
}
