package studiotools

import (
	"context"
	"fmt"

	"vozko/domain/copilot"
	"vozko/domain/studio"
	"vozko/domain/tools"
	"vozko/usecases/copilot/copilottools"
)

const (
	MinStudioListenWindowMS = 250
	MaxStudioListenWindowMS = 10_000
)

type studioListenArgs struct {
	FromMS        *int64   `json:"from_ms,omitempty" desc:"opcional: início do trecho em ms; sem ele, o começo do vídeo"`
	ToMS          *int64   `json:"to_ms,omitempty" desc:"opcional: fim do trecho em ms; sem ele, o fim do vídeo"`
	WindowMS      int64    `json:"window_ms,omitempty" desc:"opcional: tamanho de cada janela da tabela, de 250 a 10000 ms; padrão 1000"`
	VoiceTrackIDs []string `json:"voice_track_ids,omitempty" desc:"opcional: ids das faixas de áudio que são a voz (locução, fala); sem ele, as faixas que disparam o ducking"`
}

func (a studioListenArgs) issue() error {
	if a.FromMS != nil && (*a.FromMS < 0 || *a.FromMS >= studio.MaxVideoMS) {
		return fmt.Errorf("%w: from_ms fora do vídeo", copilottools.ErrInvalidArgs)
	}
	if a.ToMS != nil && (*a.ToMS <= 0 || *a.ToMS > studio.MaxVideoMS) {
		return fmt.Errorf("%w: to_ms fora do vídeo", copilottools.ErrInvalidArgs)
	}
	if a.FromMS != nil && a.ToMS != nil && *a.ToMS <= *a.FromMS {
		return fmt.Errorf("%w: to_ms precisa ser maior que from_ms", copilottools.ErrInvalidArgs)
	}
	if a.WindowMS != 0 && (a.WindowMS < MinStudioListenWindowMS || a.WindowMS > MaxStudioListenWindowMS) {
		return fmt.Errorf("%w: window_ms vai de %d a %d", copilottools.ErrInvalidArgs, MinStudioListenWindowMS, MaxStudioListenWindowMS)
	}
	return nil
}

type studioListenTool struct {
	studioScope
	deps StudioDeps
}

func NewStudioListenTool(deps StudioDeps) copilot.Tool {
	return &studioListenTool{studioScope: studioScope{kind: copilot.StudioVideo}, deps: deps}
}

func (t *studioListenTool) Meta() copilot.Meta { return studioReadMeta() }

func (t *studioListenTool) Definition() tools.Definition {
	return copilottools.Definition("studio_listen",
		"Mede o som do vídeo exatamente como ele será exportado (a mesma mixagem, com volumes, pan, cruzamentos de som, keyframes de volume, ducking e limitador) e devolve números, sem precisar ouvir: "+
			"um resumo (loudness integrada em LUFS, pico máximo em dB, quanto tempo o limitador agiu, trechos em silêncio, trechos com fala e trechos em que a música chega perto da voz) "+
			"e uma tabela por janela com loudness, pico e RMS da mixagem, se há fala, quantos dB a voz fica acima do resto, a loudness de cada faixa sozinha e, quando o som pende para um lado, balance_db (direita menos esquerda em dB; positivo pende para a direita, como o pan). "+
			"Use depois de colocar ou mexer em música, voz, efeitos ou volumes, em vez de pedir ao usuário para escutar. "+
			"Referências para redes sociais: cerca de -14 LUFS integrados (de -16 a -12 está bom), pico até -1 dB, e enquanto alguém fala a voz pelo menos 10 dB acima da música (12 dB é mais seguro).",
		studioListenArgs{})
}

func (t *studioListenTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a studioListenArgs
	if err := copilottools.DecodeArgs(args, &a); err != nil {
		return studioFailure(err.Error())
	}
	if err := a.issue(); err != nil {
		return studioFailure(err.Error())
	}
	if _, err := t.deps.openProject(ctx, cc); err != nil {
		return studioFailure(err.Error())
	}
	reply, err := runOnEditor(ctx, cc, copilot.ScreenListen, a)
	if err != nil {
		return screenResult(cc, err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: screenData(reply)}
}
