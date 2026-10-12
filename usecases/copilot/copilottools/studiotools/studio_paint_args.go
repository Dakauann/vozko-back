package studiotools

type studioPatternArgs struct {
	Preset     string   `json:"preset,omitempty" enum:"dots,grid-lines,stripes,diagonal,checker,cross,zigzag,waves,triangles,hexagons,confetti,plus" desc:"desenho pronto que se repete, pintado com color sobre background"`
	MediaID    string   `json:"media_id,omitempty" desc:"imagem da biblioteca repetida como ladrilho, no lugar de preset"`
	Color      string   `json:"color,omitempty" desc:"cor do desenho pronto #rrggbb ou #rrggbbaa"`
	Background string   `json:"background,omitempty" desc:"fundo de cada ladrilho do desenho pronto; sem ele o fill da camada aparece entre os desenhos"`
	Tile       string   `json:"tile,omitempty" enum:"grid,brick-row,brick-column,hex-row,hex-column" desc:"encaixe dos ladrilhos: grade, tijolo em linhas, tijolo em colunas, colmeia em linhas, colmeia em colunas"`
	Size       *float64 `json:"size,omitempty" desc:"largura do ladrilho em pixels da arte, de 2 a 4096 (padrão 32)"`
	SpacingX   *float64 `json:"spacing_x,omitempty" desc:"espaço horizontal entre ladrilhos em pixels; negativo sobrepõe até 90 por cento do size"`
	SpacingY   *float64 `json:"spacing_y,omitempty" desc:"espaço vertical entre ladrilhos em pixels; negativo sobrepõe até 90 por cento do size"`
	Offset     *float64 `json:"offset,omitempty" desc:"deslocamento do tijolo de 0 a 1 (padrão 0.5), só em brick-row e brick-column"`
	Angle      *float64 `json:"angle,omitempty" desc:"giro do padrão em graus, de -180 a 180"`
	OriginX    *float64 `json:"origin_x,omitempty" desc:"desliza o padrão na horizontal, em fração de um ladrilho de 0 a 1"`
	OriginY    *float64 `json:"origin_y,omitempty" desc:"desliza o padrão na vertical, em fração de um ladrilho de 0 a 1"`
	Opacity    *float64 `json:"opacity,omitempty" desc:"opacidade do padrão de 0 a 1 (padrão 1)"`
	BlendMode  string   `json:"blend_mode,omitempty" enum:"normal,multiply,screen,overlay,darken,lighten,color-dodge,add,color-burn,hard-light,soft-light,difference,exclusion,hue,saturation,color,luminosity" desc:"como o padrão se mistura com o fill embaixo"`
}

type studioGradientStopArgs struct {
	At    float64 `json:"at" desc:"tom da foto que recebe esta cor, de 0 (sombras) a 1 (luzes), em ordem crescente"`
	Color string  `json:"color" req:"true" desc:"cor #rrggbb ou #rrggbbaa; transparência deixa a foto original aparecer nesse tom"`
}

type studioGradientMapArgs struct {
	Preset        string                   `json:"preset,omitempty" enum:"noite,coral,oceano,menta,vinho,sepia,neon,pb,por-do-sol,aurora" desc:"combinação pronta: duotons noite, coral, oceano, menta, vinho, sepia, neon, pb (preto e branco) e tritons por-do-sol e aurora"`
	Stops         []studioGradientStopArgs `json:"stops,omitempty" desc:"cores do mapa, das sombras às luzes; pelo menos 2; substituem as do preset"`
	Intensity     *float64                 `json:"intensity,omitempty" desc:"quanto do mapa entra, de 0 a 1 (padrão 1)"`
	Reverse       *bool                    `json:"reverse,omitempty" desc:"inverte: as luzes recebem a primeira cor"`
	Interpolation string                   `json:"interpolation,omitempty" enum:"perceptual,srgb" desc:"perceptual (padrão) passa pelas cores com brilho uniforme; srgb mistura direto"`
}

type studioPaintArgs struct {
	Type      string              `json:"type,omitempty" enum:"solid,gradient,pattern" desc:"tipo da tinta; opcional, sai de qual campo veio entre color, gradient e pattern"`
	Color     string              `json:"color,omitempty" desc:"cor sólida #rrggbb ou #rrggbbaa"`
	Gradient  *studioGradientArgs `json:"gradient,omitempty" desc:"degradê, como no campo gradient"`
	Pattern   *studioPatternArgs  `json:"pattern,omitempty" desc:"padrão repetido, como no campo pattern"`
	Opacity   *float64            `json:"opacity,omitempty" desc:"opacidade desta tinta de 0 a 1 (padrão 1)"`
	BlendMode string              `json:"blend_mode,omitempty" enum:"normal,multiply,screen,overlay,darken,lighten,color-dodge,add,color-burn,hard-light,soft-light,difference,exclusion,hue,saturation,color,luminosity" desc:"como esta tinta se mistura com as de baixo"`
	Hidden    *bool               `json:"hidden,omitempty" desc:"esconde esta tinta sem apagar"`
}

type studioStrokeArgs struct {
	studioPaintArgs
	Width      *float64  `json:"width,omitempty" desc:"espessura em pixels da arte, de 0 a 200 (padrão 4)"`
	Align      string    `json:"align,omitempty" enum:"center,inside,outside" desc:"posição do contorno: outside fora da forma (padrão em textos, imagens e ícones), inside dentro (padrão em formas), center no meio da borda"`
	DashArray  []float64 `json:"dash_array,omitempty" desc:"tracejado em múltiplos da espessura, até 8 valores"`
	DashOffset *float64  `json:"dash_offset,omitempty" desc:"deslocamento do tracejado em múltiplos da espessura"`
	LineCap    string    `json:"line_cap,omitempty" enum:"butt,round,square" desc:"ponta do traço"`
	LineJoin   string    `json:"line_join,omitempty" enum:"miter,round,bevel" desc:"junção dos cantos"`
	MiterLimit *float64  `json:"miter_limit,omitempty" desc:"limite do canto em bico, de 1 a 20"`
}

type studioEffectArgs struct {
	ID         string   `json:"id,omitempty" desc:"id de um efeito que a camada já tem (studio_read); os campos omitidos mantêm os valores dele; sem id, nasce um efeito novo"`
	Type       string   `json:"type,omitempty" enum:"drop-shadow,inner-shadow,outer-glow,inner-glow" desc:"drop-shadow sombra atrás, inner-shadow sombra cavada por dentro, outer-glow brilho em volta, inner-glow brilho por dentro"`
	Color      string   `json:"color,omitempty" desc:"cor #rrggbb ou #rrggbbaa (sombras pretas, brilhos brancos por padrão)"`
	Opacity    *float64 `json:"opacity,omitempty" desc:"opacidade de 0 a 1"`
	X          *float64 `json:"x,omitempty" desc:"deslocamento horizontal em pixels, de -500 a 500"`
	Y          *float64 `json:"y,omitempty" desc:"deslocamento vertical em pixels, de -500 a 500"`
	Blur       *float64 `json:"blur,omitempty" desc:"desfoque em pixels, de 0 a 200"`
	Spread     *float64 `json:"spread,omitempty" desc:"expansão em pixels, de -100 a 100: nos efeitos de fora engorda a silhueta, nos de dentro avança para o centro"`
	BlendMode  string   `json:"blend_mode,omitempty" enum:"normal,multiply,screen,overlay,darken,lighten,color-dodge,add,color-burn,hard-light,soft-light,difference,exclusion,hue,saturation,color,luminosity" desc:"só inner-shadow e inner-glow: mistura com o conteúdo (inner-glow usa screen por padrão)"`
	Hidden     *bool    `json:"hidden,omitempty" desc:"esconde o efeito sem apagar"`
	ShowBehind *bool    `json:"show_behind,omitempty" desc:"só drop-shadow: a sombra aparece através das partes transparentes da camada"`
	Source     string   `json:"source,omitempty" enum:"edge,center" desc:"só inner-glow: edge brilha a partir da borda, center a partir do miolo"`
}

type studioPaintListArgs struct {
	Pattern     *studioPatternArgs     `json:"pattern,omitempty" desc:"padrão repetido sobre o fill de textos, formas e ícones; campos omitidos mantêm o padrão atual; clear pattern tira"`
	GradientMap *studioGradientMapArgs `json:"gradient_map,omitempty" desc:"só imagens: troca os tons da foto pelas cores do mapa (duotom); clear gradient_map tira"`
	Fills       []studioPaintArgs      `json:"fills,omitempty" desc:"várias tintas empilhadas em textos, formas e ícones, a primeira embaixo; substitui fill, gradient e pattern; mande a lista inteira"`
	Strokes     []studioStrokeArgs     `json:"strokes,omitempty" desc:"vários contornos, o primeiro embaixo, cada um com espessura e posição; substitui stroke e stroke_width; mande a lista inteira; em imagens e ícones o contorno segue a silhueta (cor sólida, outside)"`
	Effects     []studioEffectArgs     `json:"effects,omitempty" desc:"sombras e brilhos, o primeiro na frente; substitui shadow; mande a lista inteira"`
}
