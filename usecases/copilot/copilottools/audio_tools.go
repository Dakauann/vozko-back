package copilottools

import (
	"context"
	"strconv"

	"vozko/domain/copilot"
	"vozko/domain/mediagen"
	"vozko/domain/tools"
)

const (
	musicModelChoiceKey = "music_model"
	voiceModelChoiceKey = "voice_model"
)

type generateMusicArgs struct {
	Prompt string `json:"prompt" req:"true" desc:"como a música deve soar: gênero, clima, instrumentos, ritmo e para que serve (ex.: fundo de anúncio de cafeteria, sem voz); clipes de cerca de 30 segundos"`
}

type generateVoiceoverArgs struct {
	Script string `json:"script" req:"true" desc:"o texto exato a ser falado, no idioma do anúncio; curto (até 1500 caracteres); será lido palavra por palavra"`
	Voice  string `json:"voice" enum:"alloy,ash,ballad,coral,echo,fable,nova,onyx,sage,shimmer,verse,marin,cedar" desc:"timbre da voz; vazio usa alloy"`
}

type audioTool struct {
	media     MediaGeneration
	kind      mediagen.Kind
	name      string
	choiceKey string
	choice    copilot.ChoiceKind
}

func NewGenerateMusicTool(media MediaGeneration) copilot.Tool {
	return &audioTool{media: media, kind: mediagen.KindMusic, name: "generate_music", choiceKey: musicModelChoiceKey, choice: copilot.ChoiceMusicModel}
}

func NewGenerateVoiceoverTool(media MediaGeneration) copilot.Tool {
	return &audioTool{media: media, kind: mediagen.KindVoice, name: "generate_voiceover", choiceKey: voiceModelChoiceKey, choice: copilot.ChoiceVoiceModel}
}

func (t *audioTool) Meta() copilot.Meta { return mediaMeta() }

func (t *audioTool) Definition() tools.Definition {
	if t.kind == mediagen.KindMusic {
		return definition(t.name,
			"Gera uma música instrumental curta (cerca de 30 segundos) para anúncios e vídeos, salva na biblioteca de mídia e devolve media_id e media_url; "+
				"o media_id vai em render_video como música de fundo. O usuário escolhe o modelo no cartão de aprovação, já com o recomendado marcado. "+
				"É cobrada do saldo pelo custo do provedor, por isso só depois da aprovação do usuário.",
			generateMusicArgs{})
	}
	return definition(t.name,
		"Gera uma locução falando o roteiro exato, salva na biblioteca de mídia e devolve media_id e media_url; o media_id vai em render_video como voz. "+
			"O usuário escolhe o modelo no cartão de aprovação, já com o recomendado marcado. É cobrada do saldo pelo custo do provedor, por isso só depois da aprovação do usuário.",
		generateVoiceoverArgs{})
}

func (t *audioTool) request(cc copilot.Context, args map[string]interface{}) (mediagen.Request, error) {
	req := mediagen.Request{Kind: t.kind, WorkspaceID: cc.WorkspaceID, Model: chosenModel(args, t.choiceKey)}
	if t.kind == mediagen.KindMusic {
		a, err := validateArgs[generateMusicArgs](nil, cc, args)
		req.Prompt = a.Prompt
		return req, err
	}
	a, err := validateArgs[generateVoiceoverArgs](nil, cc, args)
	req.Prompt, req.Voice = a.Script, a.Voice
	return req, err
}

func (t *audioTool) Choices(ctx context.Context, _ copilot.Context, _ map[string]interface{}) ([]copilot.ChoiceField, error) {
	return modelChoice(ctx, t.media, t.kind, t.choiceKey, t.choice)
}

func (t *audioTool) Validate(ctx context.Context, cc copilot.Context, args map[string]interface{}) error {
	req, err := t.request(cc, args)
	if err != nil {
		return err
	}
	return checkMedia(ctx, t.media, req)
}

func (t *audioTool) Describe(_ context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	req, _ := t.request(cc, args)
	key := "music"
	if t.kind == mediagen.KindVoice {
		key = "script"
	}
	fields := []copilot.Field{{Key: key, Value: req.Prompt}}
	if req.Voice != "" {
		fields = append(fields, copilot.Field{Key: "voice", Value: req.Voice})
	}
	if req.Model != "" {
		fields = append(fields, copilot.Field{Key: t.choiceKey, Value: req.Model})
	}
	return append(fields, copilot.Field{Key: "cost", Value: "cobrado do saldo pelo custo do provedor"})
}

func (t *audioTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	req, err := t.request(cc, args)
	if err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	return generateMedia(ctx, t.media, cc, t.name, req)
}

func secondsText(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64) + " s"
}

func (t *audioTool) generationRequest(_ context.Context, cc copilot.Context, args map[string]interface{}) (mediagen.Request, error) {
	return t.request(cc, args)
}
