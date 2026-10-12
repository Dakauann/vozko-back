package studiotools

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"vozko/domain/studio"
	"vozko/usecases/copilot/copilottools"
)

var studioUnsetFields = []string{"name", "note"}

type studioOrganizeArgs struct {
	MarkerColor string   `json:"marker_color,omitempty" enum:"red,orange,yellow,green,cyan,blue,purple,pink,none" desc:"add_marker, update_marker e mark_beats: cor do marcador; none tira a cor"`
	Note        string   `json:"note,omitempty" desc:"add_marker e update_marker: nota do marcador, até 1000 caracteres (o que fazer naquele ponto)"`
	LabelColor  string   `json:"label_color,omitempty" enum:"red,orange,yellow,green,cyan,blue,purple,pink,none" desc:"update_clip e update_track: cor de etiqueta para organizar a linha do tempo (não muda a imagem); none tira"`
	Shy         *bool    `json:"shy,omitempty" desc:"update_track: true deixa a faixa discreta, escondida da linha do tempo mas ainda no vídeo e no som; false volta a mostrar"`
	Guide       *bool    `json:"guide,omitempty" desc:"update_clip: true faz do clipe um guia, que aparece na prévia e nunca entra na exportação (área segura, referência, áudio de guia); false volta ao normal; não vale para nulos"`
	BeatEvery   *int     `json:"beat_every,omitempty" desc:"mark_beats: marca uma batida a cada N (1 todas, 2 uma sim outra não, 4 uma por compasso de quatro)"`
	Unset       []string `json:"unset,omitempty" desc:"update_marker: name tira o nome e note tira a nota do marcador; update_clip: name tira o nome do clipe"`
}

func (a studioOrganizeArgs) argsIssue() string {
	switch {
	case utf8.RuneCountInString(a.Note) > studio.MaxMarkerNoteRunes:
		return fmt.Sprintf("note vai até %d caracteres", studio.MaxMarkerNoteRunes)
	case a.BeatEvery != nil && *a.BeatEvery < 1:
		return "beat_every é um inteiro a partir de 1"
	}
	for _, field := range a.Unset {
		if !copilottools.Contains(studioUnsetFields, field) {
			return "unset aceita: " + strings.Join(studioUnsetFields, ", ")
		}
	}
	return ""
}

type studioPresetArgs struct {
	PresetID     string `json:"preset_id,omitempty" desc:"apply_animation_preset: a predefinição, o preset_id exato de studio_animation_presets"`
	PresetTiming string `json:"preset_timing,omitempty" enum:"stretch,edges,keep" desc:"apply_animation_preset: stretch (padrão) estica ou encolhe a animação inteira para a duração do clipe; edges mantém a velocidade da entrada e da saída e estica só o meio; keep mantém os tempos originais e corta o que passa do fim"`
}

func (a studioPresetArgs) argsIssue(op string) string {
	if op == "apply_animation_preset" && strings.TrimSpace(a.PresetID) == "" {
		return "apply_animation_preset precisa de preset_id; liste com studio_animation_presets"
	}
	return ""
}

const studioOrganizeGuide = "Organização da linha do tempo (não muda a imagem nem o som exportados, menos guide): add_marker(at_ms, name?, duration_ms? para um trecho, marker_color?, note?, clip_id?: com clip_id o marcador fica preso ao clipe e anda com ele; at_ms é o instante na linha do tempo e precisa cair dentro do clipe), " +
	"update_marker(marker_id, clip_id? quando o id está em mais de um clipe, name?, at_ms?, duration_ms? (0 volta a ser um ponto), marker_color?, note?, unset?), remove_marker(marker_id, clip_id?), " +
	"mark_beats(clip_id de um clipe de áudio, beat_every?, marker_color?: lê a pulsação da música e põe um marcador de batida preso ao clipe em cada batida do trecho que toca, trocando as batidas marcadas antes; a resposta traz quantas e o BPM), " +
	"update_clip(name?, label_color?, guide?, unset?), update_track(label_color?, shy?). studio_read mostra markers do vídeo e, em cada clipe, name, label_color, guide e markers com at_ms na linha do tempo, duration_ms, label, color, note e beat; nas faixas, label_color e shy. " +
	"Os pontos de encaixe (snap) incluem os marcadores, o fim dos trechos e os marcadores dos clipes, então move_clip e trim_clip para o at_ms de uma batida cortam no tempo da música. "

const studioPresetGuide = "Predefinições de animação do workspace (prontas e salvas pelas pessoas): liste com studio_animation_presets, aplique com apply_animation_preset(preset_id, clip_id ou clip_ids, preset_timing?): substitui nos clipes escolhidos o que a predefinição traz (chaves de posição, escala, rotação, opacidade e desfoque relativas à caixa do clipe, comportamentos, entrada e saída, animadores de texto só em textos, efeitos, desfoque de movimento); o que um clipe não aceita fica de fora e a resposta diz o quê. Salve a animação de um clipe com studio_save_animation_preset. "
