package copilottools

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"vozko/domain/advertising"
	"vozko/domain/copilot"
	"vozko/domain/tools"
	"vozko/domain/workspace"
)

type AdSplitTests interface {
	List(ctx context.Context, workspaceID, accountID string) ([]advertising.SplitTest, error)
	Check(ctx context.Context, workspaceID string, test advertising.SplitTest) (advertising.SplitTest, error)
	Create(ctx context.Context, workspaceID string, test advertising.SplitTest) (string, error)
}

const splitTestDay = "02/01/2006"

type listAdTestsTool struct{ adGrowth }

func (t *listAdTestsTool) Meta() copilot.Meta { return adsMeta(workspace.ActionRead, false) }

func (t *listAdTestsTool) Definition() tools.Definition {
	return definition("list_ad_tests",
		"Lista os testes A/B da conta na Meta: o que cada versão compara, a divisão do público, o período e a confiança exigida.",
		adAccountArgs{})
}

func (t *listAdTestsTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a adAccountArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	account, err := t.ads.account(ctx, cc, a.AdAccountID)
	if err != nil {
		return growthFailure("list_ad_tests", "", err)
	}
	tests, err := t.deps.Tests.List(ctx, cc.WorkspaceID, account.ID)
	if err != nil {
		return growthFailure("list_ad_tests", account.ID, err)
	}
	loc, err := account.Location()
	if err != nil {
		return growthFailure("list_ad_tests", account.ID, err)
	}
	out := make([]map[string]interface{}, 0, len(tests))
	for _, test := range tests {
		cells := make([]map[string]interface{}, 0, len(test.Cells))
		for _, c := range test.Cells {
			cells = append(cells, map[string]interface{}{"name": c.Name, "share": c.Share, "meta_ids": c.ObjectIDs})
		}
		out = append(out, map[string]interface{}{
			"test_id": test.MetaID, "name": test.Name, "level": string(test.Level), "cells": cells,
			"starts": test.StartAt.In(loc).Format(splitTestDay), "ends": test.EndAt.In(loc).Format(splitTestDay), "confidence": test.Confidence,
		})
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"tests": out}}
}

type createAdTestArgs struct {
	AdAccountID string   `json:"ad_account_id" req:"true" desc:"ad_account_id de list_ad_accounts (também aceita o id da Meta ou o nome exato da conta)"`
	Name        string   `json:"name" req:"true" desc:"nome do teste"`
	Level       string   `json:"level" req:"true" enum:"campaign,adset" desc:"comparar campanhas (campaign) ou conjuntos (adset)"`
	ObjectIDs   []string `json:"object_ids" req:"true" desc:"de 2 a 5 meta_id de ads_results, do mesmo level; cada um vira uma versão do teste, com o público dividido igualmente"`
	StartDate   string   `json:"start_date" desc:"primeiro dia YYYY-MM-DD no fuso da conta; vazio ou hoje começa agora"`
	EndDate     string   `json:"end_date" req:"true" desc:"último dia YYYY-MM-DD no fuso da conta; o teste dura de 1 a 30 dias"`
	Confidence  int      `json:"confidence" desc:"confiança para declarar a versão vencedora: 65, 80, 90 ou 95 (padrão 90)"`
}

type splitTestPlan struct {
	account *advertising.AdAccount
	test    advertising.SplitTest
}

type createAdTestTool struct{ adGrowth }

func (t *createAdTestTool) Meta() copilot.Meta { return adsMeta(workspace.ActionCreate, true) }

func (t *createAdTestTool) Definition() tools.Definition {
	return definition("create_ad_test",
		"Cria um teste A/B na Meta entre campanhas ou conjuntos já publicados: a Meta divide o público sem sobreposição entre as versões e, no fim, "+
			"aponta a vencedora pelo custo por resultado. Só depois da aprovação do usuário.",
		createAdTestArgs{})
}

func (a createAdTestArgs) window(account *advertising.AdAccount, now time.Time) (time.Time, time.Time, error) {
	loc, err := account.Location()
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	endDay, err := advertising.ParseDay(strings.TrimSpace(a.EndDate))
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: end_date deve ser YYYY-MM-DD", errInvalidArgs)
	}
	_, end := advertising.DateRange{Since: endDay, Until: endDay}.Bounds(loc)
	start := now.Add(time.Minute)
	if raw := strings.TrimSpace(a.StartDate); raw != "" {
		startDay, err := advertising.ParseDay(raw)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("%w: start_date deve ser YYYY-MM-DD", errInvalidArgs)
		}
		if dayStart, _ := (advertising.DateRange{Since: startDay, Until: startDay}).Bounds(loc); dayStart.After(start) {
			start = dayStart
		}
	}
	return start, end, nil
}

func (t *createAdTestTool) plan(ctx context.Context, cc copilot.Context, args map[string]interface{}) (*splitTestPlan, error) {
	a, err := validateArgs[createAdTestArgs](nil, cc, args)
	if err != nil {
		return nil, err
	}
	account, err := t.ads.account(ctx, cc, a.AdAccountID)
	if err != nil {
		return nil, err
	}
	now := t.deps.now()
	start, end, err := a.window(account, now)
	if err != nil {
		return nil, err
	}
	ids, err := metaIDs(a.ObjectIDs)
	if err != nil {
		return nil, err
	}
	cells := make([]advertising.TestCell, 0, len(ids))
	for _, id := range ids {
		cells = append(cells, advertising.TestCell{ObjectIDs: []string{id}})
	}
	checked, err := t.deps.Tests.Check(ctx, cc.WorkspaceID, advertising.SplitTest{
		AdAccountID: account.ID, Name: a.Name, Level: advertising.TestLevel(a.Level), Cells: cells,
		StartAt: start, EndAt: end, Confidence: a.Confidence,
	})
	if err != nil {
		return nil, err
	}
	return &splitTestPlan{account: account, test: checked}, nil
}

func (t *createAdTestTool) Validate(ctx context.Context, cc copilot.Context, args map[string]interface{}) error {
	_, err := t.plan(ctx, cc, args)
	return adsValidation("create_ad_test", err)
}

func (t *createAdTestTool) Describe(ctx context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	p, err := t.plan(ctx, cc, args)
	if err != nil {
		return []copilot.Field{{Key: "test", Value: "teste com campos a corrigir"}}
	}
	loc, err := p.account.Location()
	if err != nil {
		return []copilot.Field{{Key: "test", Value: p.test.Name}}
	}
	return []copilot.Field{
		{Key: "test", Value: p.test.Name},
		{Key: "versions", Value: cellNames(p.test.Cells)},
		{Key: "period", Value: p.test.StartAt.In(loc).Format(splitTestDay) + " a " + lastDay(p.test.EndAt, loc).Format(splitTestDay)},
		{Key: "confidence", Value: strconv.Itoa(p.test.Confidence) + "%"},
	}
}

func cellNames(cells []advertising.TestCell) string {
	names := make([]string, 0, len(cells))
	for _, c := range cells {
		names = append(names, c.Name)
	}
	return strings.Join(names, ", ")
}

func (t *createAdTestTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	p, err := t.plan(ctx, cc, args)
	if err != nil {
		return growthFailure("create_ad_test", "", err)
	}
	id, err := t.deps.Tests.Create(ctx, cc.WorkspaceID, p.test)
	if err != nil {
		return growthFailure("create_ad_test", p.account.ID, err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"test_id": id, "name": p.test.Name}}
}
