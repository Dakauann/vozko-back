package studiotools

import (
	"reflect"

	"vozko/usecases/copilot/copilottools"
)

const studioTextRunGuide = "Estilo em parte de um texto: style_text(o texto alvo, layer_id na imagem ou clip_id no vídeo; char_start e char_end, ou match e match_all?; fill?, font_id?, font_weight?, font_size?, italic?, underline?, caps?, letter_spacing?, run_clear?): muda só aqueles caracteres e mantém o resto da camada; fill no trecho passa por cima de um degradê; sem char_start nem match vale para o texto inteiro, na própria camada; " +
	"num contador as posições contam o prefixo, um único caractere para o número inteiro e o sufixo; ao editar o texto depois, as letras novas herdam o estilo da letra anterior. " +
	"Formato de um trecho estilizado (cor e peso ilustrativos): {\"op\":\"style_text\",\"match\":\"50%\",\"fill\":\"#3d7a5c\",\"font_weight\":600}. "

type studioTextRunArgs struct {
	CharStart *int     `json:"char_start,omitempty" desc:"style_text: posição do primeiro caractere estilizado, contando de 0; cada caractere Unicode conta um, emoji também; vem com char_end e não junto com match"`
	CharEnd   *int     `json:"char_end,omitempty" desc:"style_text: posição logo depois do último caractere estilizado (o fim não entra)"`
	Match     string   `json:"match,omitempty" desc:"style_text: trecho do texto a estilizar; procura igual primeiro e depois sem diferenciar maiúsculas de minúsculas; sem char_start nem match o texto inteiro muda na própria camada, como update_layer, e o campo sai dos trechos"`
	MatchAll  *bool    `json:"match_all,omitempty" desc:"style_text: true estiliza todas as ocorrências de match; sem ele, só a primeira"`
	Underline *bool    `json:"underline,omitempty" desc:"style_text: sublinha o trecho"`
	Caps      *bool    `json:"caps,omitempty" desc:"style_text: mostra as letras do trecho em maiúsculas sem mudar o texto"`
	RunClear  []string `json:"run_clear,omitempty" enum:"fill,font_id,font_weight,font_size,italic,underline,caps,letter_spacing" desc:"style_text: campos que voltam ao valor da própria camada no trecho"`
}

func textRunClearFields() []string {
	field, _ := reflect.TypeOf(studioTextRunArgs{}).FieldByName("RunClear")
	return copilottools.EnumValues(field)
}
