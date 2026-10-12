package studiotools

type studioAnimatorPropertiesArgs struct {
	X         *float64 `json:"x,omitempty" desc:"deslocamento horizontal no ponto mais forte da seleção, em px do quadro, de -4000 a 4000"`
	Y         *float64 `json:"y,omitempty" desc:"deslocamento vertical em px do quadro, de -4000 a 4000 (positivo desce)"`
	Scale     *float64 `json:"scale,omitempty" desc:"escala de cada unidade no ponto mais forte, em por cento de 0 a 1000; 100 não muda nada e 0 some"`
	Rotation  *float64 `json:"rotation,omitempty" desc:"giro de cada unidade em graus, de -3600 a 3600, em volta do centro dela"`
	RotationX *float64 `json:"rotationX,omitempty" desc:"giro em profundidade de cada unidade em volta do eixo horizontal, em graus de -3600 a 3600, pelo centro dela e com a perspectiva do projeto; 90 deixa a letra de lado (some), positivo leva o topo para trás"`
	RotationY *float64 `json:"rotationY,omitempty" desc:"giro em profundidade de cada unidade em volta do eixo vertical, em graus de -3600 a 3600; 90 deixa a letra de perfil (some), 180 mostra o verso espelhado"`
	Opacity   *float64 `json:"opacity,omitempty" desc:"opacidade no ponto mais forte, em por cento de 0 a 100; 100 não muda nada e 0 esconde"`
	Blur      *float64 `json:"blur,omitempty" desc:"desfoque em px do quadro, de 0 a 100"`
	Tracking  *float64 `json:"tracking,omitempty" desc:"espaço extra depois de cada unidade em px do quadro, de -1000 a 1000; empurra as seguintes e mantém o alinhamento da linha"`
	Fill      string   `json:"fill,omitempty" desc:"cor #rrggbb do preenchimento no ponto mais forte; só em textos com cor sólida"`
	Stroke    string   `json:"stroke,omitempty" desc:"cor #rrggbb do contorno no ponto mais forte; só em textos que já têm contorno"`
}

type studioTextAnimatorArgs struct {
	AnimatorID         string                        `json:"animator_id,omitempty" desc:"update_text_animator, animate_text_animator, move_text_animator, remove_text_animator: o animador, id de studio_read (text_animators) ou do created de add_text_animator; dispensável quando o clipe tem um só"`
	AnimatorPreset     string                        `json:"animator_preset,omitempty" enum:"cascadeIn,riseIn,centerReveal,randomFade,blurIn,trackingIn,flipIn,cascadeOut,wave,colorSweep,wiggle" desc:"add_text_animator e update_text_animator: animador pronto, gravado como valores comuns e editáveis; os outros campos ajustam por cima"`
	AnimatorProperties *studioAnimatorPropertiesArgs `json:"animator_properties,omitempty" desc:"add_text_animator e update_text_animator: o que muda em cada unidade quando a seleção dela é total; propriedades omitidas ficam como estão"`
	RemoveProperties   []string                      `json:"remove_properties,omitempty" enum:"x,y,scale,rotation,rotationX,rotationY,opacity,blur,tracking,fill,stroke" desc:"update_text_animator: propriedades a tirar do animador"`
	SelectorKind       string                        `json:"selector_kind,omitempty" enum:"range,wiggly" desc:"add_text_animator e update_text_animator: range (padrão) escolhe as unidades por uma faixa; wiggly mexe cada unidade sozinho, em loop; trocar o tipo volta os valores do seletor ao padrão e tira as chaves dele"`
	RangeUnits         string                        `json:"range_units,omitempty" enum:"percent,index" desc:"seletor range: percent (padrão) mede a faixa de 0 a 100 do texto; index conta unidades (range_end 3 pega as três primeiras); trocar zera a faixa"`
	RangeStart         *float64                      `json:"range_start,omitempty" desc:"seletor range: começo da faixa, 0 a 100 em percent ou 0 a 100000 em index; com at_ms grava uma chave quando já é animado"`
	RangeEnd           *float64                      `json:"range_end,omitempty" desc:"seletor range: fim da faixa, nos mesmos limites de range_start"`
	RangeOffset        *float64                      `json:"range_offset,omitempty" desc:"seletor range: desliza a faixa, -100 a 100 em percent ou -100000 a 100000 em index; animar range_offset é o jeito comum de revelar em sequência"`
	RangeOrder         string                        `json:"range_order,omitempty" enum:"forward,backward,centerOut,edgesIn,random" desc:"seletor range: ordem das unidades; forward lê do começo, backward do fim, centerOut do meio para as pontas, edgesIn das pontas para o meio, random embaralha com selector_seed"`
	RangeShape         string                        `json:"range_shape,omitempty" enum:"square,rampUp,rampDown,triangle,round,smooth" desc:"seletor range: forma da seleção; square cheio dentro da faixa, rampUp sobe ao longo da faixa e fica cheio depois dela, rampDown o contrário, triangle, round e smooth têm o pico no meio da faixa"`
	EaseHigh           *float64                      `json:"ease_high,omitempty" desc:"seletor range: de -100 a 100; positivo achata o topo da curva da seleção, negativo deixa o topo mais pontudo"`
	EaseLow            *float64                      `json:"ease_low,omitempty" desc:"seletor range: de -100 a 100; positivo achata a base da curva, negativo a deixa mais pontuda"`
	SelectorAmount     *float64                      `json:"selector_amount,omitempty" desc:"quanto o seletor aplica: -100 a 100 no range (negativo inverte o sentido) e 0 a 100 no wiggly; com at_ms grava uma chave quando já é animado"`
	SelectorSeed       *int64                        `json:"selector_seed,omitempty" desc:"semente de 0 a 999999 da ordem random e do wiggly; a mesma semente repete o mesmo resultado"`
	WigglyRate         *float64                      `json:"wiggly_rate,omitempty" desc:"seletor wiggly: oscilações por segundo, de 0.1 a 20"`
	WigglyLoopMS       *int64                        `json:"wiggly_loop_ms,omitempty" desc:"seletor wiggly: o movimento se repete exatamente a cada wiggly_loop_ms, de 100 até o tamanho máximo do vídeo"`
	WigglyCorrelation  *float64                      `json:"wiggly_correlation,omitempty" desc:"seletor wiggly: 0 cada unidade para um lado, 100 todas juntas"`
	SelectorProperty   string                        `json:"selector_property,omitempty" enum:"start,end,offset,amount" desc:"animate_text_animator: o valor do seletor que recebe as chaves (keys com at_ms e value); o wiggly só anima amount"`
}

const studioTextAnimatorGuide = "Animadores de texto, empilháveis em qualquer quantidade num clipe de texto: add_text_animator(clip_id, animator_preset? ou unit? char (padrão), word ou line, animator_properties?, selector_kind?, os campos do seletor, to_index?) devolve o id em created; " +
	"animator_properties diz o que muda em cada unidade quando a seleção dela é total (x, y, tracking e blur em px do quadro; scale e opacity em por cento, 100 não muda nada; rotation, rotationX e rotationY em graus, os dois últimos em profundidade; fill e stroke #rrggbb); o seletor diz quanto cada unidade recebe. " +
	"Seletor range: range_units, range_start e range_end marcam a faixa, range_offset desliza, range_order escolhe a ordem (forward, backward, centerOut, edgesIn, random com selector_seed), range_shape a forma (square, rampUp, rampDown, triangle, round, smooth), ease_high e ease_low suavizam topo e base, selector_amount dosa. " +
	"Seletor wiggly: wiggly_rate, wiggly_loop_ms (repete exatamente nesse tempo), wiggly_correlation, selector_amount. " +
	"animate_text_animator(clip_id, animator_id?, selector_property, keys) anima start, end, offset ou amount: rampUp com range_end 25 e range_offset de -25 a 100 revela da primeira à última unidade; rampDown com o mesmo offset esconde nessa ordem. " +
	"update_text_animator(clip_id, animator_id?, os mesmos campos, remove_properties?, at_ms? em ms dentro do clipe para gravar uma chave quando o valor já é animado; animator_preset troca o animador pelo pronto), move_text_animator(clip_id, animator_id?, to_index), remove_text_animator(clip_id, animator_id?). " +
	"Os animadores somam posição, giro, desfoque e espaço e multiplicam escala e opacidade, em ordem; uma cor posterior pinta por cima da anterior. Prontos: cascadeIn, riseIn, centerReveal, randomFade, blurIn, trackingIn e flipIn (letras virando) entram no começo do clipe, cascadeOut sai no fim, wave passa uma onda, colorSweep passa uma faixa de cor (texto com cor sólida), wiggle treme em loop. " +
	"Valem junto com animate_text, contador e texto em caminho; studio_read mostra text_animators com estes mesmos nomes de campos. "
