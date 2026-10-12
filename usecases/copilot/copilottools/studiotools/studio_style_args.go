package studiotools

type studioStyleSet struct {
	Color         string              `json:"color,omitempty" desc:"style_kind color: a cor #rrggbb ou #rrggbbaa"`
	Gradient      *studioGradientArgs `json:"gradient,omitempty" desc:"style_kind color: um degradê no lugar de color"`
	FontID        string              `json:"font_id,omitempty" desc:"style_kind text: a fonte (os ids de font_id)"`
	FontWeight    *int                `json:"font_weight,omitempty" desc:"style_kind text: peso de 100 a 900"`
	Italic        *bool               `json:"italic,omitempty" desc:"style_kind text: true itálico, false nunca itálico"`
	FontSizePx    *float64            `json:"font_size_px,omitempty" desc:"style_kind text: tamanho da letra em pixels, de 1 a 2000; cada prancheta converte para a própria altura, então o mesmo estilo dá o mesmo tamanho no feed e no story"`
	LineHeight    *float64            `json:"line_height,omitempty" desc:"style_kind text: altura da linha, de 0.5 a 4"`
	LetterSpacing *float64            `json:"letter_spacing,omitempty" desc:"style_kind text: espaçamento entre letras em em, de -0.5 a 2"`
	Shadow        *studioShadowArgs   `json:"shadow,omitempty" desc:"style_kind effect: a sombra"`
}
