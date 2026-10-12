package studiotools

import (
	"encoding/json"
	"fmt"
	"sort"

	"vozko/domain/studio"
)

type studioVfxParamsArgs struct {
	Amount       *float64 `json:"amount,omitempty" desc:"quanto do efeito aparece, de 0 a 1, em quase todos os tipos; em chromaticAberration separa até 40 px do quadro; em glow é a força do brilho; em echo é a força do primeiro eco"`
	Size         *float64 `json:"size,omitempty" desc:"grain: tamanho do grão em px do quadro, de 0.5 a 8; lightLeak: tamanho da mancha de luz, de 0.2 a 2; glitch: altura das faixas como fração da altura, de 0.005 a 0.5; particles: raio de cada partícula em px do quadro, de 0.5 a 80; lensFlare: escala do reflexo, de 0.2 a 3; vignette: até onde o centro fica limpo, de 0 a 1; glow: raio do brilho em px do quadro, de 1 a 200; mosaic: lado de cada bloco em px do quadro, de 2 a 200; ripple, twirl e bulge: raio da área afetada como fração do lado menor, de 0 a 2; fractalNoise: tamanho das manchas em px do quadro, de 4 a 2000; audioSpectrum e audioWaveform: altura máxima das barras como fração da altura do quadro, de 0 a 1"`
	Speed        *float64 `json:"speed,omitempty" desc:"lightLeak e particles: velocidade de 0 a 4 (1 normal, 0 parado); waveWarp e ripple: ondas por segundo, de -10 a 10 (negativo anda ao contrário); fractalNoise: quanto o ruído muda por segundo, de 0 a 4; sem chaves"`
	Rate         *float64 `json:"rate,omitempty" desc:"grain: trocas do grão por segundo, de 1 a 60 (24 imita película); posterizeTime: quadros por segundo mostrados, de 1 a 30 (8 parece animação quadro a quadro); sem chaves"`
	Chroma       *float64 `json:"chroma,omitempty" desc:"grain: grão colorido de 0 (monocromático) a 1"`
	X            *float64 `json:"x,omitempty" desc:"lightLeak: de onde a luz vem; lensFlare: onde fica a fonte de luz; zoomBlur, spinBlur, ripple, twirl e bulge: o centro do efeito; gradientRamp: onde o degradê começa; audioSpectrum e audioWaveform: o centro da área das barras; de -0.5 a 1.5 na largura do quadro (fora de 0 a 1 fica fora da tela); mirror: por onde passa a linha do espelho, de 0 a 1"`
	Y            *float64 `json:"y,omitempty" desc:"como x, na altura do quadro; em audioSpectrum é a linha de base das barras e em audioWaveform o meio da onda"`
	Frequency    *float64 `json:"frequency,omitempty" desc:"glitch: surtos por segundo, de 0 a 30 (0 deixa só a separação de cor e as linhas); shake: tremidas por segundo, de 0.2 a 30; sem chaves"`
	Split        *float64 `json:"split,omitempty" desc:"glitch: separação das cores em px do quadro, de 0 a 100"`
	Scanlines    *float64 `json:"scanlines,omitempty" desc:"glitch: linhas de vídeo antigo, de 0 a 1"`
	Count        *float64 `json:"count,omitempty" desc:"particles: quantas partículas aparecem no quadro, de 1 a 1000; audioSpectrum: quantas barras, de 4 a 128; audioWaveform: quantas colunas, de 8 a 512; echo: quantos ecos, de 1 a 32; sem chaves"`
	Angle        *float64 `json:"angle,omitempty" desc:"em graus; particles: direção do movimento, de -180 a 180 (90 cai, -90 sobe, 0 vai para a direita; sem chaves); chromaticAberration: direção da separação quando radial é 0; mirror: direção da linha do espelho; directionalBlur: direção do rastro; waveWarp: para onde as ondas andam; spinBlur: arco do giro, de 0 a 180; twirl: quanto o centro torce, de -1080 a 1080"`
	Softness     *float64 `json:"softness,omitempty" desc:"particles: borda das partículas de 0 (nítida) a 1 (desfocada); vignette: suavidade da transição, de 0.01 a 1; fourColorGradient: quanto as quatro cores se misturam, de 0.01 (manchas marcadas) a 1 (tudo misturado)"`
	Twinkle      *float64 `json:"twinkle,omitempty" desc:"particles: quanto as partículas piscam, de 0 a 1"`
	Ghosts       *float64 `json:"ghosts,omitempty" desc:"lensFlare: brilho dos reflexos em linha pela lente, de 0 a 1"`
	Streak       *float64 `json:"streak,omitempty" desc:"lensFlare: risco horizontal de luz, de 0 a 1"`
	Flicker      *float64 `json:"flicker,omitempty" desc:"lensFlare: oscilação do brilho, de 0 a 1"`
	Rotation     *float64 `json:"rotation,omitempty" desc:"shake: giro máximo em graus, de 0 a 15"`
	Roundness    *float64 `json:"roundness,omitempty" desc:"vignette: 1 é um círculo, 0 segue o formato do quadro; audioSpectrum e audioWaveform: 1 arredonda as pontas das barras, 0 deixa retas"`
	Radial       *float64 `json:"radial,omitempty" desc:"chromaticAberration: 1 separa do centro para as bordas como uma lente, 0 separa numa direção só (angle); gradientRamp: 0 é um degradê linear de x,y até toX,toY e 1 é circular, com centro em x,y"`
	Threshold    *float64 `json:"threshold,omitempty" desc:"glow: a partir de que claridade a imagem brilha, de 0 (tudo brilha) a 1 (só o branco)"`
	Levels       *float64 `json:"levels,omitempty" desc:"posterize: quantos tons sobram em cada canal de cor, de 2 a 32"`
	ShiftX       *float64 `json:"shiftX,omitempty" desc:"offset: deslocamento horizontal em fração da largura, de -1 a 1; a imagem dá a volta na borda"`
	ShiftY       *float64 `json:"shiftY,omitempty" desc:"offset: deslocamento vertical em fração da altura, de -1 a 1; a imagem dá a volta na borda"`
	Length       *float64 `json:"length,omitempty" desc:"directionalBlur: comprimento do rastro em px do quadro, de 0 a 400; zoomBlur: fração do caminho até o centro que o rastro cobre, de 0 a 1; waveWarp e ripple: distância entre duas cristas em px do quadro, de 4 a 2000"`
	Height       *float64 `json:"height,omitempty" desc:"waveWarp e ripple: altura da onda em px do quadro, de 0 a 200; bulge: de -1 (aperta) a 1 (incha como uma lente)"`
	Phase        *float64 `json:"phase,omitempty" desc:"waveWarp e ripple: deslocamento da onda em graus, de 0 a 360; anime de 0 a 360 para as ondas andarem num ritmo exato"`
	TopLeftX     *float64 `json:"topLeftX,omitempty" desc:"cornerPin: x do canto superior esquerdo; fourColorGradient: x da primeira cor; em fração da largura, de -0.5 a 1.5"`
	TopLeftY     *float64 `json:"topLeftY,omitempty" desc:"cornerPin: y do canto superior esquerdo; fourColorGradient: y da primeira cor; em fração da altura, de -0.5 a 1.5"`
	TopRightX    *float64 `json:"topRightX,omitempty" desc:"cornerPin: x do canto superior direito; fourColorGradient: x da segunda cor; em fração da largura, de -0.5 a 1.5"`
	TopRightY    *float64 `json:"topRightY,omitempty" desc:"cornerPin: y do canto superior direito; fourColorGradient: y da segunda cor; em fração da altura, de -0.5 a 1.5"`
	BottomRightX *float64 `json:"bottomRightX,omitempty" desc:"cornerPin: x do canto inferior direito; fourColorGradient: x da terceira cor; em fração da largura, de -0.5 a 1.5"`
	BottomRightY *float64 `json:"bottomRightY,omitempty" desc:"cornerPin: y do canto inferior direito; fourColorGradient: y da terceira cor; em fração da altura, de -0.5 a 1.5"`
	BottomLeftX  *float64 `json:"bottomLeftX,omitempty" desc:"cornerPin: x do canto inferior esquerdo; fourColorGradient: x da quarta cor; em fração da largura, de -0.5 a 1.5"`
	BottomLeftY  *float64 `json:"bottomLeftY,omitempty" desc:"cornerPin: y do canto inferior esquerdo; fourColorGradient: y da quarta cor; em fração da altura, de -0.5 a 1.5"`
	Complexity   *float64 `json:"complexity,omitempty" desc:"fractalNoise: camadas de detalhe, de 1 (manchas lisas) a 8 (muito detalhe); valores quebrados misturam a última camada"`
	Contrast     *float64 `json:"contrast,omitempty" desc:"fractalNoise: contraste das manchas, de 0 a 4 (1 normal)"`
	Brightness   *float64 `json:"brightness,omitempty" desc:"fractalNoise: brilho das manchas, de -1 a 1 (0 não muda)"`
	ToX          *float64 `json:"toX,omitempty" desc:"gradientRamp: x de onde o degradê termina, de -0.5 a 1.5 na largura"`
	ToY          *float64 `json:"toY,omitempty" desc:"gradientRamp: y de onde o degradê termina, de -0.5 a 1.5 na altura"`
	Width        *float64 `json:"width,omitempty" desc:"audioSpectrum e audioWaveform: largura da área das barras em fração do quadro, de 0.05 a 1"`
	Gap          *float64 `json:"gap,omitempty" desc:"audioSpectrum e audioWaveform: espaço entre as barras em fração de cada coluna, de 0 a 0.9"`
	Gain         *float64 `json:"gain,omitempty" desc:"audioSpectrum e audioWaveform: ganho em dB antes de medir o som, de -24 a 24; positivo deixa as barras mais altas"`
	Window       *float64 `json:"window,omitempty" desc:"audioWaveform: quanto tempo de som a onda mostra, em ms, de 250 a 10000; sem chaves"`
	Delay        *float64 `json:"delay,omitempty" desc:"echo: tempo entre um eco e o próximo, em ms, de 10 a 1000; sem chaves"`
	Decay        *float64 `json:"decay,omitempty" desc:"echo: quanto cada eco apaga em relação ao anterior, de 0 a 1 (1 não apaga)"`
	Color        string   `json:"color,omitempty" desc:"cor #rrggbb: a luz do lightLeak, as partículas, o reflexo do lensFlare, o escuro do vignette, o tom do glow (branco mantém as cores), o escuro do fractalNoise, o começo do gradientRamp, a primeira cor do fourColorGradient, a base das barras de som"`
	Color2       string   `json:"color2,omitempty" desc:"segunda cor #rrggbb: a outra luz do lightLeak, o claro do fractalNoise, o fim do gradientRamp, a segunda cor do fourColorGradient, a ponta das barras de som"`
	Color3       string   `json:"color3,omitempty" desc:"fourColorGradient: a terceira cor em #rrggbb"`
	Color4       string   `json:"color4,omitempty" desc:"fourColorGradient: a quarta cor em #rrggbb"`
}

type studioVfxArgs struct {
	VfxID       string               `json:"vfx_id,omitempty" desc:"efeito alvo (update_vfx, animate_vfx, move_vfx, remove_vfx): id de studio_read ou @ref de add_vfx; pode faltar quando o clipe tem um efeito só"`
	VfxKind     string               `json:"vfx_kind,omitempty" enum:"audioSpectrum,audioWaveform,bulge,chromaticAberration,cornerPin,directionalBlur,echo,fourColorGradient,fractalNoise,glitch,glow,gradientRamp,grain,lensFlare,lightLeak,mirror,mosaic,offset,particles,posterize,posterizeTime,ripple,shake,spinBlur,twirl,vignette,waveWarp,zoomBlur" desc:"add_effect_clip e add_vfx. Luz: lightLeak vazamento de luz colorida, lensFlare reflexo de lente, particles partículas (poeira, neve, brilhos), glow brilho em volta das partes claras. Estilo: grain grão de película, glitch falha digital, chromaticAberration separação de cores, vignette bordas escurecidas, mosaic blocos, posterize poucos tons, mirror espelho, offset desloca e dá a volta na borda. Desfoque: directionalBlur rastro numa direção, zoomBlur rastro saindo de um ponto, spinBlur rastro girando. Distorção: waveWarp ondas, ripple ondas em círculo, twirl redemoinho, bulge lente que incha ou aperta, cornerPin prende os quatro cantos (encaixar numa tela). Gerar (num clipe de efeito na faixa de baixo vira fundo animado): fractalNoise nuvens e fumaça, gradientRamp degradê, fourColorGradient quatro cores. Tempo: shake câmera tremida, posterizeTime menos quadros por segundo, echo repete o clipe atrás dele (só em clipes, não em clipe de efeito). Som (precisam de vfx_track_id): audioSpectrum barras das frequências, audioWaveform onda do volume"`
	VfxProperty string               `json:"vfx_property,omitempty" enum:"amount,angle,bottomLeftX,bottomLeftY,bottomRightX,bottomRightY,brightness,chroma,complexity,contrast,count,decay,delay,flicker,frequency,gain,gap,ghosts,height,length,levels,phase,radial,rate,rotation,roundness,scanlines,shiftX,shiftY,size,softness,speed,split,streak,threshold,toX,toY,topLeftX,topLeftY,topRightX,topRightY,twinkle,width,window,x,y" desc:"animate_vfx: o valor do efeito que as keys animam; rate, speed, frequency, count, delay, window e o angle das particles não recebem chaves"`
	VfxParams   *studioVfxParamsArgs `json:"vfx_params,omitempty" desc:"add_effect_clip, add_vfx e update_vfx: os valores a mudar; cada tipo aceita só os seus (studio_read mostra todos com o valor atual)"`
	VfxSeed     *int                 `json:"vfx_seed,omitempty" desc:"inteiro de 0 a 999999 que escolhe a variação aleatória do efeito (sem ele, um sorteado); o mesmo seed dá sempre o mesmo resultado"`
	VfxHidden   *bool                `json:"vfx_hidden,omitempty" desc:"update_vfx: true desliga o efeito sem apagar"`
	VfxTrackID  string               `json:"vfx_track_id,omitempty" desc:"audioSpectrum e audioWaveform (add_effect_clip, add_vfx, update_vfx): a faixa de áudio que as barras mostram, id de studio_read ou @ref; num clipe dentro de um grupo, uma faixa de áudio do grupo"`
}

func paramValues(params any) map[string]any {
	raw, err := json.Marshal(params)
	if err != nil {
		return nil
	}
	values := map[string]any{}
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil
	}
	return values
}

func vfxParamsIssue(kind string, params any, allowed func(string) bool) string {
	if kind == "" || params == nil {
		return ""
	}
	values := paramValues(params)
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if text, isColor := values[name].(string); isColor {
			if !studio.VfxColorAllowed(kind, name, text) {
				return fmt.Sprintf("vfx_params.%s não é uma cor de %s; use #rrggbb nas cores do tipo", name, kind)
			}
			continue
		}
		value, _ := values[name].(float64)
		low, high, ok := studio.VfxParamRange(kind, name)
		if !ok || !allowed(name) {
			return fmt.Sprintf("vfx_params.%s não vale para %s", name, kind)
		}
		if studio.VfxParamIssue(kind, name, value) != "" {
			return fmt.Sprintf("vfx_params.%s de %s vai de %g a %g", name, kind, low, high)
		}
	}
	return ""
}

func soundKind(kind string) bool {
	for _, sound := range studio.SoundVfxKinds() {
		if sound == kind {
			return true
		}
	}
	return false
}

func (a studioVfxArgs) argsIssue(op string) string {
	if a.VfxParams != nil {
		if msg := vfxParamsIssue(a.VfxKind, a.VfxParams, func(string) bool { return true }); msg != "" {
			return msg
		}
	}
	if a.VfxKind == "" {
		return ""
	}
	if a.VfxTrackID != "" && !soundKind(a.VfxKind) {
		return "vfx_track_id vale só para audioSpectrum e audioWaveform"
	}
	if soundKind(a.VfxKind) && a.VfxTrackID == "" && (op == "add_vfx" || op == "add_effect_clip") {
		return a.VfxKind + " precisa de vfx_track_id, a faixa de áudio que as barras mostram"
	}
	if a.VfxKind == "echo" && op == "add_effect_clip" {
		return "echo repete o próprio clipe e não vale num clipe de efeito; use add_vfx no clipe que se move"
	}
	return ""
}
