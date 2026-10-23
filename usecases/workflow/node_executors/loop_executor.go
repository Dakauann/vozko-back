package node_executors

import (
	"encoding/json"
	"fmt"
	"reflect"

	"vozko/domain/workflow"
)

type loopExecutor struct{}

func NewLoopExecutor() workflow.NodeExecutor {
	return &loopExecutor{}
}

func (e *loopExecutor) Definition() workflow.NodeDefinition {
	return workflow.NodeDefinition{
		Type:         workflow.NodeTypeActionLoop,
		TestBehavior: workflow.NodeTestPure,
		Category:     workflow.NodeCategoryLogic,
		Scopes:       []workflow.NodeScope{workflow.NodeScopeShared, workflow.NodeScopeVoice},
		Label:        "Loop / Iteração",
		Description:  "Itera sobre uma lista (array JSON). A cada passagem define o item atual e o índice.",
		Icon:         "ArrowsClockwise",
		Guidance: workflow.NodeGuidance{
			When:     "Para iterar sobre uma lista (list_variable).",
			Behavior: "Percorre a lista item a item: a saída 'body' executa uma vez por item (normalmente retornando ao próprio loop para iterar) e a saída 'done' segue ao terminar. Um ciclo de volta ao loop é válido; o loop garante o término.",
		},
		Outputs: []workflow.HandleDefinition{
			{ID: "body", Label: "Corpo do loop"},
			{ID: "done", Label: "Concluído"},
			{ID: "fail", Label: "Falha", Optional: true},
		},
		DefaultConfig: map[string]interface{}{
			"list_variable": "",
			"item_variable": "item",
		},
		OutputKeys: []workflow.OutputKeyDefinition{
			{Key: "item", Description: "Item atual da iteração", Label: "Item atual", Type: workflow.VariableTypeUnknown},
			{Key: "index", Description: "Índice da iteração atual (0-based)", Label: "Posição do item", Type: workflow.VariableTypeNumber},
			{Key: "total", Description: "Total de itens na lista", Label: "Total de itens", Type: workflow.VariableTypeNumber},
			{Key: "is_first", Description: "true se é o primeiro item", Label: "É o primeiro", Type: workflow.VariableTypeBoolean},
			{Key: "is_last", Description: "true se é o último item", Label: "É o último", Type: workflow.VariableTypeBoolean},
		},
		ConfigSchema: []workflow.ConfigField{
			{Key: "list_variable", Label: "Variável da lista", Type: "text", Placeholder: "var.items", Required: true},
			{Key: "item_variable", Label: "Nome do item", Type: "text", Placeholder: "item"},
		},
	}
}

func (e *loopExecutor) Execute(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
	listVar, _ := ctx.Node.Config["list_variable"].(string)
	itemVar, _ := ctx.Node.Config["item_variable"].(string)

	if listVar == "" {
		return nil, workflow.ErrNodeConfigMissing
	}
	if itemVar == "" {
		itemVar = "item"
	}

	rawVal, found := workflow.ResolveVariable(listVar, ctx.State)
	var list []interface{}
	if found {
		list = toSlice(rawVal)
	}
	if list == nil {

		return routeOutput(&workflow.NodeResult{
			Output: map[string]interface{}{
				"item": nil, "index": 0, "total": 0,
				"is_first": true, "is_last": true,
			},
		}, ctx.Graph.OutgoingEdges(ctx.Node.ID), "fail"), nil
	}

	total := len(list)

	indexKey := fmt.Sprintf("_loop_%s_index", ctx.Node.ID)

	if prevID, ok := ctx.State.Get("_prev_node_id"); ok {
		if pid, ok := prevID.(string); ok {
			if pid != ctx.Node.ID && !isInLoopBody(ctx.Graph, ctx.Node.ID, pid) {
				ctx.State.Delete(indexKey)
			}
		} else {

			ctx.State.Delete(indexKey)
		}
	}

	currentIndex := 0
	if idx, ok := ctx.State.Get(indexKey); ok {
		currentIndex = parseLoopIndex(idx)
	}

	if currentIndex >= total {

		ctx.State.Delete(indexKey)
		return routeOutput(&workflow.NodeResult{
			Output: map[string]interface{}{
				"item": nil, "index": currentIndex, "total": total,
				"is_first": false, "is_last": true,
			},
		}, ctx.Graph.OutgoingEdges(ctx.Node.ID), "done"), nil
	}

	item := list[currentIndex]
	ctx.State.Set(itemVar, item)
	ctx.State.Set(itemVar+"_index", currentIndex)

	ctx.State.Set(indexKey, float64(currentIndex+1))

	return routeOutput(&workflow.NodeResult{
		Output: map[string]interface{}{
			"item":     item,
			"index":    currentIndex,
			"total":    total,
			"is_first": currentIndex == 0,
			"is_last":  currentIndex == total-1,
		},
	}, ctx.Graph.OutgoingEdges(ctx.Node.ID), "body"), nil
}

func toSlice(val interface{}) []interface{} {
	if val == nil {
		return nil
	}
	if slice, ok := val.([]interface{}); ok {
		return slice
	}
	rv := reflect.ValueOf(val)
	if rv.IsValid() {
		kind := rv.Kind()
		if kind == reflect.Slice || kind == reflect.Array {
			out := make([]interface{}, rv.Len())
			for i := 0; i < rv.Len(); i++ {
				out[i] = rv.Index(i).Interface()
			}
			return out
		}
	}
	if str, ok := val.(string); ok {
		var arr []interface{}
		if err := json.Unmarshal([]byte(str), &arr); err == nil {
			return arr
		}
	}
	return nil
}

func parseLoopIndex(v interface{}) int {
	switch n := v.(type) {
	case int:
		return n
	case int8:
		return int(n)
	case int16:
		return int(n)
	case int32:
		return int(n)
	case int64:
		return int(n)
	case uint:
		return int(n)
	case uint8:
		return int(n)
	case uint16:
		return int(n)
	case uint32:
		return int(n)
	case uint64:
		return int(n)
	case float32:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}

func isInLoopBody(graph *workflow.Graph, loopNodeID, prevNodeID string) bool {
	bodyTarget := resolveEdgeByLabelStrict(graph.OutgoingEdges(loopNodeID), "body")
	if bodyTarget == "" {
		return false
	}

	visited := map[string]bool{loopNodeID: true}
	queue := []string{bodyTarget}
	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]
		if visited[curr] {
			continue
		}
		visited[curr] = true
		if curr == prevNodeID {
			return true
		}

		node := graph.FindNode(curr)
		if node != nil && (node.Type.AwaitsContact() || node.Type.IsTrigger()) {
			continue
		}
		for _, e := range graph.OutgoingEdges(curr) {
			if !visited[e.Target] {
				queue = append(queue, e.Target)
			}
		}
	}
	return false
}
