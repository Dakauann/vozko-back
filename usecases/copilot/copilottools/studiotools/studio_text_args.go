package studiotools

type studioCounterArgs struct {
	CounterValue  *float64 `json:"counter_value,omitempty" desc:"set_counter: número mostrado quando o contador não é animado, de -1e12 a 1e12; com at_ms em ms dentro do clipe grava uma chave quando o contador já é animado; não vem junto com counter_from nem counter_to"`
	CounterFrom   *float64 `json:"counter_from,omitempty" desc:"set_counter: número de onde a contagem parte; com counter_to conta entre os dois a partir de count_start_ms durante count_ms, com easing (padrão easeOut)"`
	CounterTo     *float64 `json:"counter_to,omitempty" desc:"set_counter: número onde a contagem chega e para"`
	CountStartMS  *int64   `json:"count_start_ms,omitempty" desc:"set_counter: quando a contagem começa, em ms dentro do clipe (padrão 0)"`
	CountMS       *int64   `json:"count_ms,omitempty" desc:"set_counter: duração da contagem em ms (padrão até o fim do clipe)"`
	Decimals      *int     `json:"decimals,omitempty" desc:"set_counter: casas decimais, de 0 a 3"`
	CounterLocale string   `json:"counter_locale,omitempty" enum:"pt-BR,en-US,es-ES,de-DE" desc:"set_counter: separador de milhar e marca decimal; pt-BR, es-ES e de-DE escrevem 1.234,5 e en-US escreve 1,234.5"`
	Grouping      *bool    `json:"grouping,omitempty" desc:"set_counter: false tira o separador de milhar (anos, códigos)"`
	Prefix        *string  `json:"prefix,omitempty" desc:"set_counter: texto antes do número, até 16 caracteres, como R$; texto vazio remove"`
	Suffix        *string  `json:"suffix,omitempty" desc:"set_counter: texto depois do número, até 16 caracteres, como % ou mil; texto vazio remove"`
}

type studioTextPathArgs struct {
	PathShape   string   `json:"path_shape,omitempty" enum:"arc,circle,wave,custom" desc:"set_text_path: curva que o texto segue; arc é um arco com o mesmo visual de curve, que ele substitui; circle contorna uma elipse dentro da caixa; wave é uma onda; custom segue path com coordenadas de 0 a 1 na caixa do clipe"`
	Bend        *float64 `json:"bend,omitempty" desc:"set_text_path: só em arc e wave, de -1 a 1; no arc positivo arqueia para cima como um arco-íris e negativo curva para baixo; na wave é a altura da onda"`
	PathOffset  *float64 `json:"path_offset,omitempty" desc:"set_text_path: onde o texto fica ao longo da curva, fração do comprimento de -4 a 4 (0 início, 0.5 meio, 1 fim); com at_ms em ms dentro do clipe grava uma chave quando a posição já é animada"`
	PathAlign   string   `json:"path_align,omitempty" enum:"start,center,end" desc:"set_text_path: que ponto do texto fica em path_offset: start, center (padrão) ou end"`
	Upright     *bool    `json:"upright,omitempty" desc:"set_text_path: true mantém as letras em pé em vez de girarem com a curva"`
	PathReverse *bool    `json:"path_reverse,omitempty" desc:"set_text_path: true inverte o sentido da curva; no circle o texto passa a ler por dentro"`
}
