package studiotools

import "vozko/domain/studio"

type studioStillVfxParamsArgs struct {
	Amount       *float64 `json:"amount,omitempty" desc:"quanto do efeito aparece, de 0 a 1, em todos os tipos; em chromaticAberration separa até 40 px da caixa; em glow é a força do brilho"`
	Size         *float64 `json:"size,omitempty" desc:"grain: tamanho do grão em px da arte, de 0.5 a 8; lightLeak: tamanho da mancha de luz, de 0.2 a 2; glitch: altura das faixas como fração da altura, de 0.005 a 0.5; lensFlare: escala do reflexo, de 0.2 a 3; vignette: até onde o centro fica limpo, de 0 a 1; glow: raio do brilho em px da arte, de 1 a 200; mosaic: lado de cada bloco em px da arte, de 2 a 200; ripple, twirl e bulge: raio da área afetada como fração do lado menor, de 0 a 2; fractalNoise: tamanho das manchas em px da arte, de 4 a 2000"`
	Chroma       *float64 `json:"chroma,omitempty" desc:"grain: grão colorido de 0 (monocromático) a 1"`
	X            *float64 `json:"x,omitempty" desc:"lightLeak: de onde a luz vem; lensFlare: onde fica a fonte de luz; zoomBlur, spinBlur, ripple, twirl e bulge: o centro do efeito; gradientRamp: onde o degradê começa; de -0.5 a 1.5 na largura da foto ou da prancheta (fora de 0 a 1 fica fora da arte); mirror: por onde passa a linha do espelho, de 0 a 1"`
	Y            *float64 `json:"y,omitempty" desc:"como x, na altura"`
	Split        *float64 `json:"split,omitempty" desc:"glitch: separação das cores em px da arte, de 0 a 100"`
	Scanlines    *float64 `json:"scanlines,omitempty" desc:"glitch: linhas de vídeo antigo, de 0 a 1"`
	Angle        *float64 `json:"angle,omitempty" desc:"em graus; chromaticAberration: direção da separação quando radial é 0; mirror: direção da linha do espelho; directionalBlur: direção do rastro; waveWarp: direção das ondas; spinBlur: arco do giro, de 0 a 180; twirl: quanto o centro torce, de -1080 a 1080"`
	Softness     *float64 `json:"softness,omitempty" desc:"vignette: suavidade da transição, de 0.01 a 1; fourColorGradient: quanto as quatro cores se misturam, de 0.01 a 1"`
	Roundness    *float64 `json:"roundness,omitempty" desc:"vignette: 1 é um círculo, 0 segue o formato da arte"`
	Radial       *float64 `json:"radial,omitempty" desc:"chromaticAberration: 1 separa do centro para as bordas como uma lente, 0 separa numa direção só (angle); gradientRamp: 0 é linear de x,y até toX,toY e 1 é circular com centro em x,y"`
	Ghosts       *float64 `json:"ghosts,omitempty" desc:"lensFlare: brilho dos reflexos em linha pela lente, de 0 a 1"`
	Streak       *float64 `json:"streak,omitempty" desc:"lensFlare: risco horizontal de luz, de 0 a 1"`
	Threshold    *float64 `json:"threshold,omitempty" desc:"glow: a partir de que claridade a imagem brilha, de 0 (tudo) a 1 (só o branco)"`
	Levels       *float64 `json:"levels,omitempty" desc:"posterize: quantos tons sobram em cada canal, de 2 a 32"`
	ShiftX       *float64 `json:"shiftX,omitempty" desc:"offset: deslocamento horizontal em fração da largura, de -1 a 1; a imagem dá a volta na borda"`
	ShiftY       *float64 `json:"shiftY,omitempty" desc:"offset: deslocamento vertical em fração da altura, de -1 a 1"`
	Length       *float64 `json:"length,omitempty" desc:"directionalBlur: comprimento do rastro em px da arte, de 0 a 400; zoomBlur: fração do caminho até o centro, de 0 a 1; waveWarp e ripple: distância entre cristas em px da arte, de 4 a 2000"`
	Height       *float64 `json:"height,omitempty" desc:"waveWarp e ripple: altura da onda em px da arte, de 0 a 200; bulge: de -1 (aperta) a 1 (incha)"`
	Phase        *float64 `json:"phase,omitempty" desc:"waveWarp e ripple: deslocamento da onda em graus, de 0 a 360"`
	TopLeftX     *float64 `json:"topLeftX,omitempty" desc:"cornerPin: x do canto superior esquerdo; fourColorGradient: x da primeira cor; em fração da largura, de -0.5 a 1.5"`
	TopLeftY     *float64 `json:"topLeftY,omitempty" desc:"cornerPin: y do canto superior esquerdo; fourColorGradient: y da primeira cor; em fração da altura"`
	TopRightX    *float64 `json:"topRightX,omitempty" desc:"cornerPin: x do canto superior direito; fourColorGradient: x da segunda cor"`
	TopRightY    *float64 `json:"topRightY,omitempty" desc:"cornerPin: y do canto superior direito; fourColorGradient: y da segunda cor"`
	BottomRightX *float64 `json:"bottomRightX,omitempty" desc:"cornerPin: x do canto inferior direito; fourColorGradient: x da terceira cor"`
	BottomRightY *float64 `json:"bottomRightY,omitempty" desc:"cornerPin: y do canto inferior direito; fourColorGradient: y da terceira cor"`
	BottomLeftX  *float64 `json:"bottomLeftX,omitempty" desc:"cornerPin: x do canto inferior esquerdo; fourColorGradient: x da quarta cor"`
	BottomLeftY  *float64 `json:"bottomLeftY,omitempty" desc:"cornerPin: y do canto inferior esquerdo; fourColorGradient: y da quarta cor"`
	Complexity   *float64 `json:"complexity,omitempty" desc:"fractalNoise: camadas de detalhe, de 1 a 8"`
	Contrast     *float64 `json:"contrast,omitempty" desc:"fractalNoise: contraste das manchas, de 0 a 4 (1 normal)"`
	Brightness   *float64 `json:"brightness,omitempty" desc:"fractalNoise: brilho das manchas, de -1 a 1"`
	ToX          *float64 `json:"toX,omitempty" desc:"gradientRamp: x de onde o degradê termina, de -0.5 a 1.5"`
	ToY          *float64 `json:"toY,omitempty" desc:"gradientRamp: y de onde o degradê termina, de -0.5 a 1.5"`
	Color        string   `json:"color,omitempty" desc:"cor #rrggbb: a luz do lightLeak, o reflexo do lensFlare, o escuro do vignette, o tom do glow, o escuro do fractalNoise, o começo do gradientRamp, a primeira cor do fourColorGradient"`
	Color2       string   `json:"color2,omitempty" desc:"segunda cor #rrggbb: a outra luz do lightLeak, o claro do fractalNoise, o fim do gradientRamp, a segunda cor do fourColorGradient"`
	Color3       string   `json:"color3,omitempty" desc:"fourColorGradient: a terceira cor em #rrggbb"`
	Color4       string   `json:"color4,omitempty" desc:"fourColorGradient: a quarta cor em #rrggbb"`
}

type studioStillVfxArgs struct {
	VfxID     string                    `json:"vfx_id,omitempty" desc:"efeito alvo (update_vfx, move_vfx, remove_vfx): id de studio_read ou @ref de add_vfx; pode faltar quando a foto ou a prancheta tem um efeito só"`
	VfxKind   string                    `json:"vfx_kind,omitempty" enum:"bulge,chromaticAberration,cornerPin,directionalBlur,fourColorGradient,fractalNoise,glitch,glow,gradientRamp,grain,lensFlare,lightLeak,mirror,mosaic,offset,posterize,ripple,spinBlur,twirl,vignette,waveWarp,zoomBlur" desc:"add_vfx. Luz: lightLeak vazamento de luz colorida, lensFlare reflexo de lente, glow brilho em volta das partes claras. Estilo: grain grão de película, glitch falha digital, chromaticAberration separação de cores, vignette bordas escurecidas, mosaic blocos, posterize poucos tons, mirror espelho, offset desloca e dá a volta na borda. Desfoque: directionalBlur, zoomBlur e spinBlur. Distorção: waveWarp ondas, ripple ondas em círculo, twirl redemoinho, bulge lente, cornerPin prende os quatro cantos (encaixar a foto numa tela de celular ou num quadro). Gerar: fractalNoise nuvens e fumaça, gradientRamp degradê, fourColorGradient quatro cores (na prancheta vale sobre tudo, o fundo incluído)"`
	VfxParams *studioStillVfxParamsArgs `json:"vfx_params,omitempty" desc:"add_vfx e update_vfx: os valores a mudar; cada tipo aceita só os seus (studio_read mostra todos com o valor atual)"`
	VfxSeed   *int                      `json:"vfx_seed,omitempty" desc:"inteiro de 0 a 999999 que escolhe a variação do grão, da luz, da falha, dos reflexos ou do ruído (sem ele, um sorteado); o mesmo seed dá sempre a mesma imagem"`
	VfxHidden *bool                     `json:"vfx_hidden,omitempty" desc:"update_vfx: true desliga o efeito sem apagar"`
	ToIndex   *int                      `json:"to_index,omitempty" desc:"add_vfx e move_vfx: posição do efeito na lista, 0 é o primeiro a ser aplicado"`
}

func (a studioStillVfxArgs) argsIssue() string {
	if a.VfxParams == nil {
		return ""
	}
	allowed := map[string]bool{}
	for _, name := range studio.StillVfxParams(a.VfxKind) {
		allowed[name] = true
	}
	return vfxParamsIssue(a.VfxKind, a.VfxParams, func(name string) bool { return allowed[name] })
}
