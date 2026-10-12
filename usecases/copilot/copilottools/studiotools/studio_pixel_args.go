package studiotools

type studioPixelArgs struct {
	Action       string          `json:"action,omitempty" enum:"fill,erase,hide,reveal" desc:"paint_area: fill pinta a área com paint_color numa camada de pintura; erase apaga a pintura da área (numa foto, esconde); hide esconde a área de uma foto pela máscara, sem perder pixels; reveal mostra de novo o que estava escondido"`
	Area         *studioAreaArgs `json:"area,omitempty" desc:"paint_area, crop_to_area e copy_area_to_layer: a região, em frações da prancheta"`
	PaintColor   string          `json:"paint_color,omitempty" desc:"paint_area com fill: a cor #rrggbb"`
	PaintOpacity *float64        `json:"paint_opacity,omitempty" desc:"paint_area: força de 0 a 1, padrão 1"`
}

type studioAreaArgs struct {
	Shape   string            `json:"shape,omitempty" enum:"rect,ellipse,polygon" desc:"rect é um retângulo, ellipse uma elipse e polygon um contorno livre pelos pontos de points"`
	X       *float64          `json:"x,omitempty" desc:"rect e ellipse: centro horizontal em frações da prancheta, como as camadas"`
	Y       *float64          `json:"y,omitempty" desc:"rect e ellipse: centro vertical em frações da prancheta, como as camadas"`
	W       *float64          `json:"w,omitempty" desc:"rect e ellipse: largura em frações da prancheta, como as camadas"`
	H       *float64          `json:"h,omitempty" desc:"rect e ellipse: altura em frações da prancheta, como as camadas"`
	Points  []studioAreaPoint `json:"points,omitempty" desc:"polygon: os vértices em frações da prancheta, pelo menos 3"`
	Feather *float64          `json:"feather,omitempty" desc:"suaviza a borda, em pixels da prancheta, de 0 a 250"`
	Invert  *bool             `json:"invert,omitempty" desc:"true usa tudo fora da área"`
}

type studioAreaPoint struct {
	X float64 `json:"x" req:"true" desc:"posição horizontal, 0 (esquerda) a 1 (direita)"`
	Y float64 `json:"y" req:"true" desc:"posição vertical, 0 (topo) a 1 (base)"`
}
