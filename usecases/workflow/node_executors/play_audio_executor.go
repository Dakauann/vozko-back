package node_executors

import (
	"context"
	"errors"
	"strings"

	"vozko/domain/workflow"
)

type playAudioExecutor struct {
	audio workflow.VoiceAudio
}

func NewPlayAudioExecutor(audio workflow.VoiceAudio) workflow.NodeExecutor {
	return &playAudioExecutor{audio: audio}
}

func (e *playAudioExecutor) Definition() workflow.NodeDefinition {
	return workflow.NodeDefinition{
		Type:        workflow.NodeTypeActionPlayAudio,
		Category:    workflow.NodeCategoryAction,
		Scopes:      []workflow.NodeScope{workflow.NodeScopeVoice},
		Label:       "Tocar Áudio",
		Description: "Toca um áudio da biblioteca de mídias para quem está na ligação.",
		Icon:        "SpeakerHigh",
		Guidance: workflow.NodeGuidance{
			When: "Para falar com quem ligou: saudação, menu de opções ou aviso. Só existe em fluxos de voz.",
			Behavior: "Toca o áudio escolhido (media_id, use find_resource medias com um arquivo de áudio). Com 'interruptible' " +
				"(padrão), uma tecla pressionada corta o áudio e fica guardada para o próximo wait_dtmf, como numa URA.",
			Examples: []string{
				"config: {\"media_id\":\"<id>\",\"interruptible\":true}  // depois conecte a um wait_dtmf para ler a opção",
			},
		},
		DefaultConfig: map[string]interface{}{"media_id": "", "interruptible": true},
		OutputKeys: []workflow.OutputKeyDefinition{
			{Key: "interrupted", Description: "Verdadeiro quando uma tecla cortou o áudio"},
		},
		ConfigSchema: []workflow.ConfigField{
			{Key: "media_id", Label: "Áudio", Type: "select", OptionsSource: "medias", Required: true, Description: "Arquivo de áudio da biblioteca de mídias."},
			{Key: "interruptible", Label: "Tecla interrompe o áudio", Type: "boolean", Description: "Quem ligou pode escolher a opção sem ouvir o áudio até o fim."},
		},
	}
}

func (e *playAudioExecutor) Execute(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
	call, err := workflow.VoiceCallFrom(ctx)
	if err != nil {
		return voiceNodeError(err)
	}
	if e.audio == nil {
		return voiceNodeError(workflow.ErrAudioNotPlayable)
	}
	mediaID, _ := ctx.Node.Config["media_id"].(string)
	pcm, err := e.audio.LoadPCM(context.Background(), ctx.Workflow.WorkspaceID, strings.TrimSpace(mediaID))
	if err != nil {
		return voiceNodeError(err)
	}
	interruptible, set := ctx.Node.Config["interruptible"].(bool)
	interrupted, err := call.Play(pcm, interruptible || !set)
	if err != nil {
		return voiceNodeError(err)
	}
	return &workflow.NodeResult{Output: map[string]interface{}{"interrupted": interrupted}}, nil
}

func voiceNodeError(err error) (*workflow.NodeResult, error) {
	if errors.Is(err, context.Canceled) {
		return nil, err
	}
	return &workflow.NodeResult{Error: err.Error()}, nil
}
