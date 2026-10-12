package studiotools

type studioClipStyleArgs struct {
	Fills          []studioPaintArgs  `json:"fills,omitempty" desc:"update_clip em textos, formas e ícones: várias tintas empilhadas, a primeira embaixo; substitui fill e gradient; mande a lista inteira"`
	Strokes        []studioStrokeArgs `json:"strokes,omitempty" desc:"update_clip: vários contornos, o primeiro embaixo; em clipes de vídeo e imagem o contorno segue a silhueta (borda da imagem, máscaras e partes transparentes), só cor sólida e align outside; mande a lista inteira"`
	Effects        []studioEffectArgs `json:"effects,omitempty" desc:"update_clip: sombras e brilhos, o primeiro na frente; em textos, formas e ícones os quatro tipos, em clipes de vídeo e imagem só drop-shadow e outer-glow, em volta da silhueta; um id existente mantém os valores e as chaves dele; mande a lista inteira"`
	EffectID       string             `json:"effect_id,omitempty" desc:"animate_effect: id do efeito (studio_read); pode faltar quando o clipe tem um efeito só"`
	EffectProperty string             `json:"effect_property,omitempty" enum:"opacity,x,y,blur,spread" desc:"animate_effect: valor do efeito que as keys animam; opacity de 0 a 1, x e y de -500 a 500 px, blur de 0 a 200 px, spread de -100 a 100 px"`
}

const studioClipStyleGuide = "Estilos de camada no vídeo: update_clip com fills, strokes e effects funciona como no editor de imagem em textos, formas e ícones; em clipes de vídeo e imagem strokes e effects (drop-shadow e outer-glow) seguem a silhueta do clipe, com a borda da imagem, o recorte das máscaras e as partes transparentes de PNGs. " +
	"animate_effect(clip_id, effect_id?, effect_property, keys com value e at_ms dentro do clipe) anima um valor do efeito com as curvas das chaves: um brilho que pulsa (opacity ou spread), uma sombra que anda (x e y) ou cresce (blur). Keys vazias tiram a animação desse valor. studio_read traz effects e strokes do clipe e as chaves de cada efeito. "

func (o studioVideoOperation) clipStyleIssue() string {
	if o.Op != "animate_effect" {
		if o.EffectID != "" || o.EffectProperty != "" {
			return "effect_id e effect_property valem só para animate_effect"
		}
		return ""
	}
	if o.EffectProperty == "" {
		return "animate_effect precisa de effect_property: opacity, x, y, blur ou spread"
	}
	for _, k := range o.Keys {
		if k.Value == nil {
			return "as chaves de animate_effect precisam de value"
		}
	}
	return ""
}
