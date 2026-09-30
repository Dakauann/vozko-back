package node_executors

import (
	"time"

	"vozko/domain/workflow"
)

const (
	dtmfTimeoutHandle   = "timeout"
	dtmfInvalidHandle   = "invalid"
	dtmfTimeoutDefault  = 8 * time.Second
	dtmfTimeoutMaxLimit = 60 * time.Second
)

type waitDTMFExecutor struct{}

func NewWaitDTMFExecutor() workflow.NodeExecutor {
	return &waitDTMFExecutor{}
}

func (e *waitDTMFExecutor) Definition() workflow.NodeDefinition {
	keyOptions := make([]workflow.ConfigFieldOption, 0, len(workflow.DTMFKeys))
	for _, key := range workflow.DTMFKeys {
		keyOptions = append(keyOptions, workflow.ConfigFieldOption{Value: string(key), Label: string(key)})
	}
	return workflow.NodeDefinition{
		Type:        workflow.NodeTypeWaitDTMF,
		Category:    workflow.NodeCategoryWait,
		Scopes:      []workflow.NodeScope{workflow.NodeScopeVoice},
		Label:       "Aguardar Tecla",
		Description: "Espera quem ligou apertar uma tecla do telefone e segue pela opção escolhida.",
		Icon:        "Hash",
		Guidance: workflow.NodeGuidance{
			When: "Para ler a opção de um menu de voz (URA). Coloque depois de um action_play_audio que anuncia as opções.",
			Behavior: "Saídas DINÂMICAS: cada tecla em 'keys' (0-9, * ou #) vira uma saída com o próprio dígito como rótulo; " +
				"conecte todas. 'invalid' recebe teclas fora da lista e 'timeout' recebe o silêncio após 'timeout_seconds'; " +
				"se não forem conectadas, a ligação é encerrada. Para repetir o menu, ligue 'invalid' ou 'timeout' de volta ao áudio.",
			Examples: []string{
				"config: {\"keys\":[\"1\",\"2\"],\"timeout_seconds\":8}  // arestas: \"1\", \"2\", \"invalid\", \"timeout\"",
			},
		},
		DefaultConfig: map[string]interface{}{"keys": []interface{}{}, "timeout_seconds": float64(dtmfTimeoutDefault / time.Second)},
		Outputs: []workflow.HandleDefinition{
			{ID: dtmfInvalidHandle, Label: "Tecla inválida", Optional: true},
			{ID: dtmfTimeoutHandle, Label: "Sem resposta", Optional: true},
		},
		DynamicHandles: true,
		OutputKeys: []workflow.OutputKeyDefinition{
			{Key: "key", Description: "Tecla pressionada (vazio quando ninguém apertou)"},
		},
		ConfigSchema: []workflow.ConfigField{
			{Key: "keys", Label: "Teclas", Type: "multi-select", Options: keyOptions, Required: true, Description: "Cada tecla escolhida vira uma saída."},
			{Key: "timeout_seconds", Label: "Esperar até (segundos)", Type: "number", Placeholder: "8"},
		},
	}
}

func (e *waitDTMFExecutor) Execute(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
	call, err := workflow.VoiceCallFrom(ctx)
	if err != nil {
		return voiceNodeError(err)
	}
	key, pressed, err := call.NextKey(dtmfTimeout(ctx.Node.Config))
	if err != nil {
		return voiceNodeError(err)
	}
	branch := dtmfBranch(workflow.DTMFKeysOf(ctx.Node.Config), key, pressed)
	output := map[string]interface{}{"key": ""}
	if pressed {
		output["key"] = string(key)
	}
	next := resolveEdgeByLabelStrict(ctx.Graph.OutgoingEdges(ctx.Node.ID), branch)
	if next == "" {
		return &workflow.NodeResult{Output: output, Complete: true}, nil
	}
	return &workflow.NodeResult{NextNodeID: next, Output: output}, nil
}

func dtmfBranch(keys []string, key rune, pressed bool) string {
	if !pressed {
		return dtmfTimeoutHandle
	}
	for _, allowed := range keys {
		if allowed == string(key) {
			return allowed
		}
	}
	return dtmfInvalidHandle
}

func dtmfTimeout(config map[string]interface{}) time.Duration {
	seconds, _ := config["timeout_seconds"].(float64)
	timeout := time.Duration(seconds * float64(time.Second))
	if timeout <= 0 {
		return dtmfTimeoutDefault
	}
	return min(timeout, dtmfTimeoutMaxLimit)
}

func WaitDTMFOutputs(config map[string]interface{}) []workflow.HandleDefinition {
	keys := workflow.DTMFKeysOf(config)
	outputs := make([]workflow.HandleDefinition, 0, len(keys)+2)
	for _, key := range keys {
		if workflow.IsDTMFKey(key) {
			outputs = append(outputs, workflow.HandleDefinition{ID: key, Label: key})
		}
	}
	return append(outputs,
		workflow.HandleDefinition{ID: dtmfInvalidHandle, Label: "Tecla inválida", Optional: true},
		workflow.HandleDefinition{ID: dtmfTimeoutHandle, Label: "Sem resposta", Optional: true},
	)
}
