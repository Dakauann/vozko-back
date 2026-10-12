package studiotools

type studioTangentArgs struct {
	X float64 `json:"x" req:"true" desc:"deslocamento horizontal da alça a partir da chave, em frações da largura do quadro, de -2 a 2"`
	Y float64 `json:"y" req:"true" desc:"deslocamento vertical da alça a partir da chave, em frações da altura do quadro, de -2 a 2"`
}

type studioAnchorArgs struct {
	Anchor     string   `json:"anchor,omitempty" enum:"top-left,top,top-right,left,center,right,bottom-left,bottom,bottom-right" desc:"set_anchor: ponto pronto da âncora na caixa do clipe; center é o padrão"`
	AnchorX    *float64 `json:"anchor_x,omitempty" desc:"set_anchor: âncora horizontal como fração da caixa do clipe, de -1 a 2: 0 é a borda esquerda, 0.5 o centro, 1 a borda direita; fora de 0 a 1 gira em volta de um ponto fora do clipe; vence o x de anchor"`
	AnchorY    *float64 `json:"anchor_y,omitempty" desc:"set_anchor: âncora vertical como fração da caixa do clipe, de -1 a 2: 0 é a borda de cima, 0.5 o centro, 1 a borda de baixo; vence o y de anchor"`
	KeepVisual *bool    `json:"keep_visual,omitempty" desc:"set_anchor: true (padrão) mantém a imagem onde está e corrige a posição; false só move a âncora e o clipe passa a girar e crescer em volta dela a partir do lugar atual"`
	FlipX      *bool    `json:"flip_x,omitempty" desc:"update_clip: espelha a imagem do clipe na horizontal dentro da própria caixa, sem mover o clipe"`
	FlipY      *bool    `json:"flip_y,omitempty" desc:"update_clip: espelha a imagem do clipe na vertical dentro da própria caixa, sem mover o clipe"`
	AutoOrient *bool    `json:"auto_orient,omitempty" desc:"update_clip: true soma à rotação a direção do trajeto curvo (position), então o clipe aponta para onde anda; sem position não muda nada"`
	Separate   *bool    `json:"separate,omitempty" desc:"scale_axes: true troca scale por scaleX e scaleY com as mesmas chaves (sem scale animado, começa com uma chave 1 no cursor de tempo), para animar largura e altura em separado; false junta scaleX e scaleY de volta em scale"`
	KeepAxis   string   `json:"keep_axis,omitempty" enum:"x,y" desc:"scale_axes com separate false: qual eixo vira scale quando scaleX e scaleY têm chaves diferentes"`
	Curved     *bool    `json:"curved,omitempty" desc:"set_path: true junta as chaves de x e y num trajeto curvo (position) com curvas automáticas; false volta para x e y separados; um trajeto curvo vira chaves lineares de x e y que seguem a curva"`
}
