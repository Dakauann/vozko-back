package copilottools

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"vozko/domain/copilot"
	"vozko/domain/mediagen"
	"vozko/domain/studio"
	"vozko/domain/tools"
)

const (
	MaxStudioOperations = 40
	maxStudioKeys       = 32
)

type studioBoxArgs struct {
	X        *float64 `json:"x,omitempty" desc:"centro horizontal, 0 (esquerda) a 1 (direita)"`
	Y        *float64 `json:"y,omitempty" desc:"centro vertical, 0 (topo) a 1 (base)"`
	W        *float64 `json:"w,omitempty" desc:"largura como fração da largura do quadro"`
	H        *float64 `json:"h,omitempty" desc:"altura como fração da altura do quadro"`
	Rotation *float64 `json:"rotation,omitempty" desc:"rotação em graus"`
	Opacity  *float64 `json:"opacity,omitempty" desc:"opacidade de 0 a 1"`
}

type studioStyleArgs struct {
	Text          string               `json:"text,omitempty" desc:"texto (criar ou trocar)"`
	Fill          string               `json:"fill,omitempty" desc:"cor de preenchimento #rrggbb ou #rrggbbaa"`
	Stroke        string               `json:"stroke,omitempty" desc:"cor do contorno #rrggbb"`
	StrokeWidth   *float64             `json:"stroke_width,omitempty" desc:"espessura do contorno em pixels do quadro"`
	FontID        string               `json:"font_id,omitempty" desc:"fonte"`
	FontSize      *float64             `json:"font_size,omitempty" desc:"altura da letra como fração da altura do quadro (0,04 legenda, 0,08 título)"`
	FontWeight    *int                 `json:"font_weight,omitempty" desc:"peso de 100 a 900"`
	Align         string               `json:"align,omitempty" enum:"left,center,right" desc:"alinhamento do texto"`
	Shape         string               `json:"shape,omitempty" enum:"rect,ellipse,line,arrow,triangle,star,path" desc:"forma (add_shape); path desenha o caminho vetorial de path"`
	Path          string               `json:"path,omitempty" desc:"caminho vetorial SVG (shape path no add_shape, ou update_layer de uma forma path): comandos M L H V C S Q T A Z, números separados por espaço, coordenadas de 0 a 1 dentro da caixa da camada; ex.: M0 0.6 L1 0.4 L1 1 L0 1 Z"`
	PathPreset    string               `json:"path_preset,omitempty" enum:"diagonalBand,diagonalSplit,arch,ribbon,chevron,wave,blob,speechBubble,heart,swoosh,tornPaper,cornerBracket,squiggle,burst,seal" desc:"forma vetorial pronta (add_shape), no lugar de shape"`
	FillRule      string               `json:"fill_rule,omitempty" enum:"nonzero,evenodd" desc:"forma path: evenodd vaza as sobreposições (o miolo do O, um anel); nonzero, o padrão, preenche tudo"`
	LineCap       string               `json:"line_cap,omitempty" enum:"butt,round,square" desc:"ponta do traço"`
	LineJoin      string               `json:"line_join,omitempty" enum:"miter,round,bevel" desc:"junção dos cantos do traço"`
	MiterLimit    *float64             `json:"miter_limit,omitempty" desc:"limite do canto em bico quando line_join é miter, de 1 a 20"`
	DashArray     []float64            `json:"dash_array,omitempty" desc:"padrão do tracejado em múltiplos da espessura: [3,2] tracejado, [0,2] com line_cap round faz pontos; até 8 valores; [] volta ao traço contínuo"`
	DashOffset    *float64             `json:"dash_offset,omitempty" desc:"deslocamento do tracejado em múltiplos da espessura"`
	Points        *int                 `json:"points,omitempty" desc:"pontas da estrela, de 3 a 64"`
	Inner         *float64             `json:"inner,omitempty" desc:"raio interno da estrela como fração da ponta, de 0.05 a 1 (0.45 padrão)"`
	Italic        *bool                `json:"italic,omitempty" desc:"texto em itálico"`
	LineHeight    *float64             `json:"line_height,omitempty" desc:"altura da linha do texto, de 0.5 a 4 (1.2 normal)"`
	LetterSpacing *float64             `json:"letter_spacing,omitempty" desc:"espaçamento entre letras como fração do tamanho da letra, de -0.5 a 2"`
	Radius        *float64             `json:"radius,omitempty" desc:"arredondamento dos cantos de 0 a 1 (retângulos e imagens)"`
	Dash          *bool                `json:"dash,omitempty" desc:"contorno tracejado"`
	ArrowStart    *bool                `json:"arrow_start,omitempty" desc:"ponta de seta no início (linhas, setas e caminhos abertos)"`
	ArrowEnd      *bool                `json:"arrow_end,omitempty" desc:"ponta de seta no fim (linhas, setas e caminhos abertos)"`
	Shadow        *studioShadowArgs    `json:"shadow,omitempty" desc:"sombra; informe ao menos um campo, e os omitidos usam a sombra suave padrão (#00000066, blur 12, y 6); objeto vazio é ignorado"`
	Gradient      *studioGradientArgs  `json:"gradient,omitempty" desc:"degradê no texto ou na forma; trocar fill por uma cor sólida remove o degradê"`
	Highlight     *studioHighlightArgs `json:"highlight,omitempty" desc:"caixa colorida atrás do texto"`
	Curve         *float64             `json:"curve,omitempty" desc:"curva do texto em arco, de -1 a 1"`
	BlendMode     string               `json:"blend_mode,omitempty" enum:"normal,multiply,screen,overlay,darken,lighten,color-dodge,color-burn,hard-light,soft-light,difference,exclusion,hue,saturation,color,luminosity" desc:"mistura com o que está embaixo"`
}

type studioShadowArgs struct {
	Color string   `json:"color,omitempty" desc:"cor #rrggbbaa; prefira preto com transparência, como #00000059"`
	Blur  *float64 `json:"blur,omitempty" desc:"desfoque em pixels da arte, de 0 a 200; cerca de 15 a 25 por cento do tamanho da letra"`
	X     *float64 `json:"x,omitempty" desc:"deslocamento horizontal em pixels, de -500 a 500"`
	Y     *float64 `json:"y,omitempty" desc:"deslocamento vertical em pixels, de -500 a 500"`
}

type studioGradientArgs struct {
	Kind   string   `json:"kind,omitempty" enum:"linear,radial" desc:"linear (padrão) ou radial, do centro para fora"`
	From   string   `json:"from" req:"true" desc:"cor inicial #rrggbb ou #rrggbbaa (no radial, a do centro)"`
	Via    string   `json:"via,omitempty" desc:"cor do meio, opcional"`
	To     string   `json:"to" req:"true" desc:"cor final (no radial, a da borda)"`
	Angle  *float64 `json:"angle,omitempty" desc:"linear: ângulo em graus, 0 da esquerda para a direita, 90 de cima para baixo"`
	CX     *float64 `json:"cx,omitempty" desc:"radial: centro horizontal dentro da camada, de 0 a 1 (padrão 0.5)"`
	CY     *float64 `json:"cy,omitempty" desc:"radial: centro vertical dentro da camada, de 0 a 1 (padrão 0.5)"`
	Radius *float64 `json:"radius,omitempty" desc:"radial: raio, de 0.05 a 2; 1 alcança a borda da camada"`
}

type studioHighlightArgs struct {
	Color  string   `json:"color" req:"true" desc:"cor da caixa atrás do texto"`
	Radius *float64 `json:"radius,omitempty" desc:"arredondamento da caixa de 0 a 1"`
}

type studioFiltersArgs struct {
	Brightness *float64 `json:"brightness,omitempty" desc:"brilho de -1 a 1"`
	Contrast   *float64 `json:"contrast,omitempty" desc:"contraste de -100 a 100"`
	Saturation *float64 `json:"saturation,omitempty" desc:"saturação de -2 a 10"`
	Blur       *float64 `json:"blur,omitempty" desc:"desfoque de 0 a 40"`
}

type studioCropArgs struct {
	X float64 `json:"x" desc:"início horizontal do recorte na imagem original, de 0 a 1"`
	Y float64 `json:"y" desc:"início vertical do recorte, de 0 a 1"`
	W float64 `json:"w" req:"true" desc:"largura do recorte, de 0.01 a 1; x + w vai até 1"`
	H float64 `json:"h" req:"true" desc:"altura do recorte, de 0.01 a 1; y + h vai até 1"`
}

type studioKeyArgs struct {
	AtMS   int64   `json:"at_ms" req:"true" desc:"instante da chave em ms, contado do início do clipe: de 0 até a duração do clipe"`
	Value  float64 `json:"value" req:"true" desc:"x e y de -1 a 2 (0.5 é o centro; fora de 0 a 1 sai da tela); scale de 0.05 a 5 (1 é o tamanho atual; o clipe animado não pode passar de 4 vezes o quadro); rotation de -3600 a 3600 graus; opacity de 0 a 1"`
	Easing string  `json:"easing,omitempty" desc:"curva do trecho que sai desta chave, padrão easeInOut: linear, hold, easeIn, easeOut, easeInOut, backIn (recua antes de sair), backOut (passa do ponto e volta), backInOut, elastic (vibra), bounce (quica), spring (chega com impulso de mola) ou uma curva css cubic-bezier(x1,y1,x2,y2) com x de 0 a 1 e y de -1 a 2, como cubic-bezier(0.05,0.7,0.1,1) para uma entrada enfática"`
}

type studioVideoOperation struct {
	Op         string          `json:"op" req:"true" enum:"add_media,add_text,add_shape,add_icon,add_captions,move_clip,trim_clip,slip_clip,split_clip,delete_clips,duplicate_clips,update_clip,animate,motion_preset,entrance,exit,add_track,move_track,update_track,remove_track,add_marker,set_canvas,seek,select" desc:"a operação"`
	Ref        string          `json:"ref,omitempty" desc:"nome curto para o clipe ou faixa que esta operação cria; as operações seguintes usam @nome"`
	ClipID     string          `json:"clip_id,omitempty" desc:"clipe alvo: id de studio_read ou @ref"`
	ClipIDs    []string        `json:"clip_ids,omitempty" desc:"vários clipes (delete_clips, duplicate_clips, update_clip, motion_preset, entrance, exit, select)"`
	TrackID    string          `json:"track_id,omitempty" desc:"faixa alvo ou de destino: id de studio_read ou @ref"`
	MediaID    string          `json:"media_id,omitempty" desc:"mídia da biblioteca (add_media) ou o arquivo de legendas pronto (add_captions)"`
	IconID     string          `json:"icon_id,omitempty" desc:"ícone do catálogo (add_icon); studio_read full lista os ids"`
	AtMS       *int64          `json:"at_ms,omitempty" desc:"instante na linha do tempo em ms: onde inserir, cortar, marcar ou posicionar o cursor de tempo"`
	StartMS    *int64          `json:"start_ms,omitempty" desc:"novo início do clipe na linha do tempo (move_clip, trim_clip)"`
	EndMS      *int64          `json:"end_ms,omitempty" desc:"novo fim do clipe na linha do tempo (trim_clip)"`
	DurationMS *int64          `json:"duration_ms,omitempty" desc:"duração em ms (textos, formas, imagens)"`
	TrimInMS   *int64          `json:"trim_in_ms,omitempty" desc:"de que ponto do arquivo de origem o clipe começa, em ms (slip_clip)"`
	ToIndex    *int            `json:"to_index,omitempty" desc:"posição da faixa, 0 é a de baixo (move_track, add_track)"`
	Style      string          `json:"style,omitempty" enum:"headline,caption,cta,lowerThird,tag" desc:"estilo pronto de texto (add_text)"`
	Preset     string          `json:"preset,omitempty" enum:"kenBurns,enterLeft,slideUp,fadeIn,fadeOut,pulse,spin,wobble,pop,drop,springIn" desc:"animação pronta por keyframes (motion_preset); pop cresce passando do tamanho, drop cai e quica, springIn entra com mola"`
	Effect     string          `json:"effect,omitempty" enum:"none,fade,slideUp,slideDown,slideLeft,slideRight" desc:"efeito de entrada ou de saída (entrance, exit)"`
	Property   string          `json:"property,omitempty" enum:"x,y,scale,rotation,opacity" desc:"propriedade animada (animate)"`
	Keys       []studioKeyArgs `json:"keys,omitempty" desc:"chaves da animação (animate); substituem as chaves dessa propriedade; vazio remove a animação dela"`
	Volume     *float64        `json:"volume,omitempty" desc:"volume de 0 a 2 (1 normal)"`
	FadeInMS   *int64          `json:"fade_in_ms,omitempty" desc:"duração do fade de entrada em ms"`
	FadeOutMS  *int64          `json:"fade_out_ms,omitempty" desc:"duração do fade de saída em ms"`
	Fit        string          `json:"fit,omitempty" enum:"cover,contain" desc:"cover preenche o quadro cortando; contain mostra inteiro"`
	Kind       string          `json:"kind,omitempty" enum:"visual,audio" desc:"tipo da faixa (add_track)"`
	Name       string          `json:"name,omitempty" desc:"nome da faixa ou rótulo do marcador"`
	Hidden     *bool           `json:"hidden,omitempty" desc:"esconder a faixa"`
	Locked     *bool           `json:"locked,omitempty" desc:"travar a faixa"`
	Muted      *bool           `json:"muted,omitempty" desc:"silenciar a faixa"`
	Disabled   *bool           `json:"disabled,omitempty" desc:"desativar o clipe sem apagar"`
	Ripple     *bool           `json:"ripple,omitempty" desc:"delete_clips: puxar o que vem depois para fechar o buraco"`
	Aspect     string          `json:"aspect,omitempty" enum:"square,portrait,story,landscape" desc:"formato do vídeo (set_canvas)"`
	Background string          `json:"background,omitempty" desc:"cor de fundo do vídeo #rrggbb (set_canvas)"`
	Clear      []string        `json:"clear,omitempty" enum:"shadow,stroke,gradient,highlight,curve,blend_mode" desc:"efeitos a remover do texto ou da forma"`
	studioBoxArgs
	studioStyleArgs
}

type studioImageOperation struct {
	Op           string             `json:"op" req:"true" enum:"add_text,add_shape,add_image,add_icon,update_layer,delete_layers,duplicate_layers,order_layers,align_layers,distribute_layers,group_layers,ungroup_layers,scaffold_layers,set_hidden,set_locked,replace_image,update_artboard,add_artboard,duplicate_artboard,delete_artboard,move_to_artboard,select,combine_shapes" desc:"a operação"`
	Ref          string             `json:"ref,omitempty" desc:"nome curto para o que esta operação cria (camada, grupo ou prancheta); as operações seguintes usam @nome; depois de duplicate_artboard, @nome/id_da_camada_original aponta para a cópia dessa camada"`
	LayerID      string             `json:"layer_id,omitempty" desc:"camada alvo: id de studio_read ou @ref"`
	ArtboardID   string             `json:"artboard_id,omitempty" desc:"prancheta: id de studio_read ou @ref; nas criações escolhe onde a camada nasce (sem ele, na prancheta ativa); obrigatório em delete_artboard e no destino de move_to_artboard"`
	LayerIDs     []string           `json:"layer_ids,omitempty" desc:"várias camadas (delete, duplicate, order, align, distribute, group, ungroup, scaffold, set_hidden, set_locked, select, update_layer, combine_shapes)"`
	MediaID      string             `json:"media_id,omitempty" desc:"imagem da biblioteca, inclusive anexos da conversa (add_image, replace_image)"`
	IconID       string             `json:"icon_id,omitempty" desc:"ícone do catálogo (add_icon); studio_read full lista os ids"`
	Style        string             `json:"style,omitempty" enum:"heading,subheading,body" desc:"estilo pronto de texto (add_text)"`
	Direction    string             `json:"direction,omitempty" enum:"forward,backward,front,back" desc:"order_layers: um passo para frente, um para trás, para o topo, para o fundo"`
	AboveID      string             `json:"above_id,omitempty" desc:"order_layers: coloca as camadas logo acima desta (id ou @ref)"`
	BelowID      string             `json:"below_id,omitempty" desc:"order_layers: coloca as camadas logo abaixo desta (id ou @ref)"`
	Alignment    string             `json:"alignment,omitempty" enum:"left,center,right,top,middle,bottom" desc:"align_layers"`
	Axis         string             `json:"axis,omitempty" enum:"horizontal,vertical" desc:"distribute_layers"`
	Frame        string             `json:"frame,omitempty" enum:"ellipse,triangle,star,none" desc:"recorta a imagem no formato; none tira"`
	Clip         *bool              `json:"clip,omitempty" desc:"máscara: true mostra a camada só dentro da camada logo abaixo dela"`
	FlipX        *bool              `json:"flip_x,omitempty" desc:"espelha a imagem na horizontal"`
	FlipY        *bool              `json:"flip_y,omitempty" desc:"espelha a imagem na vertical"`
	Filters      *studioFiltersArgs `json:"filters,omitempty" desc:"ajustes da imagem; campos omitidos mantêm o valor atual"`
	FilterPreset string             `json:"filter_preset,omitempty" enum:"vivid,soft,faded,dramatic,bright" desc:"ajuste pronto da imagem"`
	Crop         *studioCropArgs    `json:"crop,omitempty" desc:"recorte da imagem original; o que fica continua no mesmo lugar"`
	Clear        []string           `json:"clear,omitempty" enum:"shadow,stroke,gradient,highlight,curve,crop,filters,frame,clip,blend_mode" desc:"efeitos a remover"`
	Hidden       *bool              `json:"hidden,omitempty" desc:"esconder (set_hidden)"`
	Locked       *bool              `json:"locked,omitempty" desc:"travar (set_locked)"`
	Width        *int               `json:"width,omitempty" desc:"largura da prancheta em pixels, de 100 a 4096 (add_artboard, update_artboard, duplicate_artboard para a versão em outro formato)"`
	Height       *int               `json:"height,omitempty" desc:"altura da prancheta em pixels, de 100 a 4096 (add_artboard, update_artboard, duplicate_artboard)"`
	Background   string             `json:"background,omitempty" desc:"cor de fundo da prancheta #rrggbb ou transparent (add_artboard, update_artboard); remove o degradê do fundo"`
	Name         string             `json:"name,omitempty" desc:"nome da camada ou da prancheta (add_artboard, duplicate_artboard, update_artboard)"`
	Mode         string             `json:"mode,omitempty" enum:"union,subtract,intersect,exclude,flatten" desc:"combine_shapes: union une as formas, subtract tira as de cima da mais baixa, intersect deixa só a sobreposição, exclude tira a sobreposição, flatten junta tudo num caminho sem cortar"`
	studioBoxArgs
	studioStyleArgs
}

type studioVideoEditArgs struct {
	Operations []studioVideoOperation `json:"operations" req:"true" desc:"de 1 a 40 operações (prefira 8 a 12 por lote), aplicadas em ordem e juntas: se uma falhar, nenhuma é aplicada"`
}

type studioImageEditArgs struct {
	Operations []studioImageOperation `json:"operations" req:"true" desc:"de 1 a 40 operações (prefira 8 a 12 por lote), aplicadas em ordem e juntas: se uma falhar, nenhuma é aplicada"`
}

const studioVideoEditGuide = "Edita o projeto de vídeo aberto como uma pessoa faria no editor, com o seu cursor visível, em um único passo que o usuário desfaz com Ctrl+Z. " +
	"Operações e campos: add_media(media_id, at_ms, track_id?, duration_ms?: vídeo traz o áudio junto), " +
	"add_text(text, at_ms, duration_ms?, track_id?, style?, x?, y?, w?, h?, fill?, gradient?, stroke?, font_id?, font_size?, font_weight?, italic?, align?, line_height?, letter_spacing?, highlight?, curve?, shadow?, blend_mode?), " +
	"add_shape(shape ou path_preset, path? quando shape é path, points? e inner? na estrela, at_ms, duration_ms?, track_id?, x, y, w, h, fill? ou gradient?, stroke?, stroke_width?, radius?, dash?, shadow?, blend_mode?), " +
	"add_icon(icon_id, at_ms, duration_ms?, track_id?, x?, y?, w?, h?, fill?), add_captions(media_id do arquivo .vtt, clip_id do clipe falado), " +
	"move_clip(clip_id, start_ms, track_id?), trim_clip(clip_id, start_ms? e ou end_ms?), slip_clip(clip_id, trim_in_ms), split_clip(clip_id, at_ms), " +
	"delete_clips(clip_ids, ripple?), duplicate_clips(clip_ids), update_clip(clip_id ou clip_ids, volume?, fade_in_ms?, fade_out_ms?, fit?, disabled?, x?, y?, w?, h?, rotation?, opacity?, text? e estilo: italic, line_height, letter_spacing, radius, dash, arrow_start, arrow_end, shadow, gradient, highlight, curve, blend_mode, clear), " +
	"animate(clip_id, property, keys), motion_preset(clip_id ou clip_ids, preset), entrance(clip_id ou clip_ids, effect, duration_ms?), exit(clip_id ou clip_ids, effect, duration_ms?), " +
	"add_track(kind, to_index?, name?), move_track(track_id, to_index), update_track(track_id, name?, hidden?, locked?, muted?), remove_track(track_id), " +
	"add_marker(at_ms, name?), set_canvas(aspect? e ou background?), seek(at_ms), select(clip_ids). " +
	"Exemplo de título animado: [{\"op\":\"add_text\",\"ref\":\"titulo\",\"text\":\"Oferta de hoje\",\"style\":\"headline\",\"at_ms\":0,\"duration_ms\":3000,\"y\":0.3}," +
	"{\"op\":\"entrance\",\"clip_id\":\"@titulo\",\"effect\":\"slideUp\",\"duration_ms\":400},{\"op\":\"animate\",\"clip_id\":\"@titulo\",\"property\":\"scale\"," +
	"\"keys\":[{\"at_ms\":400,\"value\":1},{\"at_ms\":2600,\"value\":1.08}]}]. Em cada operação, mande só os campos que ela usa. " +
	"Devolve os ids criados, os clipes alterados, os campos ignorados por não valerem para o clipe (ignored) e a checagem automática de design."

const studioImageEditGuide = "Edita o projeto de imagem aberto como uma pessoa faria no editor, com o seu cursor visível, em um único passo que o usuário desfaz com Ctrl+Z. " +
	"As camadas vão de baixo para cima: a primeira é o fundo e a última fica na frente de todas (studio_read mostra z). " +
	"Operações e campos: add_text(text, style?, x?, y?, w?, h?, fill?, gradient?, stroke?, font_id?, font_size?, font_weight?, italic?, align?, line_height?, letter_spacing?, highlight?, curve?, shadow?), " +
	"add_shape(shape ou path_preset, path? quando shape é path, points? e inner? na estrela, x, y, w, h, fill? ou gradient?, stroke?, stroke_width?, dash?, radius?, shadow?), add_image(media_id, x?, y?, w?, h?, radius?, frame?, filters?), add_icon(icon_id, x?, y?, w?, h?, fill?), " +
	"update_layer(layer_id ou layer_ids, qualquer campo de posição, texto, estilo ou efeito: gradient, shadow, highlight, curve, blend_mode, frame, clip, flip_x, flip_y, filters, filter_preset, crop, clear, name), " +
	"delete_layers(layer_ids), duplicate_layers(layer_ids), order_layers(layer_ids, direction ou above_id ou below_id), align_layers(layer_ids, alignment), distribute_layers(layer_ids, axis), " +
	"group_layers(layer_ids), ungroup_layers(layer_ids), scaffold_layers(layer_ids: a camada de baixo vira o elemento pai das outras), " +
	"set_hidden(layer_ids, hidden), set_locked(layer_ids, locked), replace_image(layer_id, media_id), select(layer_ids ou artboard_id), " +
	"combine_shapes(layer_ids, mode: troca formas e caminhos por um caminho novo, como as operações booleanas do Figma; a ordem de baixo para cima decide o subtract). " +
	"Máscara: coloque a forma logo abaixo da imagem (order_layers com below_id) e ligue clip na imagem. " +
	"Pranchetas: o projeto tem uma ou mais, lado a lado (studio_read lista; active_artboard é a que o usuário está vendo). x, y, w e h das camadas são frações da prancheta onde elas estão. " +
	"As operações de camada acham a prancheta pelo id da camada, e uma operação mexe numa prancheta só; as de criação usam artboard_id ou a prancheta ativa. " +
	"add_artboard(width, height, name?, background? ou gradient?), duplicate_artboard(artboard_id?, name?, width? e height?: a cópia em outro formato reorganiza as camadas em vez de esticar), " +
	"update_artboard(artboard_id?, name?, width? e height?, background? ou gradient?), delete_artboard(artboard_id), move_to_artboard(layer_ids, artboard_id). " +
	"A prancheta criada ou duplicada fica selecionada e passa a ser a ativa; a resposta traz copies com o id de cada camada copiada pelo id original. " +
	"Exemplo de versão: [{\"op\":\"duplicate_artboard\",\"ref\":\"v2\",\"name\":\"Versão escura\"},{\"op\":\"update_artboard\",\"artboard_id\":\"@v2\",\"background\":\"#111111\"},{\"op\":\"update_layer\",\"layer_id\":\"@v2/ID_DO_TITULO\",\"fill\":\"#ffffff\"}]. " +
	"Exemplo de círculo com degradê radial: {\"op\":\"add_shape\",\"shape\":\"ellipse\",\"x\":0.5,\"y\":0.5,\"w\":0.4,\"h\":0.4,\"gradient\":{\"kind\":\"radial\",\"from\":\"#fff7cc\",\"via\":\"#ff8a00\",\"to\":\"#7a1f00\"}}. " +
	"Em cada operação, mande só os campos que ela usa. Devolve os ids criados, as camadas alteradas, os campos ignorados por não valerem para a camada (ignored) e a checagem automática de design."

type studioEditTool struct {
	studioScope
	deps    StudioDeps
	name    string
	guide   string
	args    any
	allowed []string
}

func NewStudioEditVideoTool(deps StudioDeps) copilot.Tool {
	return &studioEditTool{studioScope: studioScope{kind: copilot.StudioVideo}, deps: deps, name: "studio_edit_video", guide: studioVideoEditGuide, args: studioVideoEditArgs{}, allowed: opEnum(studioVideoOperation{})}
}

func NewStudioEditImageTool(deps StudioDeps) copilot.Tool {
	return &studioEditTool{studioScope: studioScope{kind: copilot.StudioImage}, deps: deps, name: "studio_edit_image", guide: studioImageEditGuide, args: studioImageEditArgs{}, allowed: opEnum(studioImageOperation{})}
}

func opEnum(op any) []string {
	field, _ := reflect.TypeOf(op).FieldByName("Op")
	return enumValues(field)
}

func (t *studioEditTool) Meta() copilot.Meta { return studioEditMeta() }

func (t *studioEditTool) LogsArguments() bool { return true }

func (t *studioEditTool) Definition() tools.Definition {
	def := definition(t.name, t.guide, t.args)
	ops := def.Parameters["operations"]
	if ops.Items != nil {
		if font, ok := ops.Items.Properties["font_id"]; ok {
			font.Enum = sortedFonts()
			ops.Items.Properties["font_id"] = font
		}
	}
	def.Parameters["operations"] = ops
	return def
}

func sortedFonts() []string {
	fonts := studio.Fonts()
	sort.Strings(fonts)
	return fonts
}

func (t *studioEditTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	operations, err := t.operations(args)
	if err != nil {
		return studioFailure(err.Error())
	}
	if _, err := t.deps.openProject(ctx, cc); err != nil {
		return studioFailure(err.Error())
	}
	reply, err := runOnEditor(ctx, cc, copilot.ScreenEdit, map[string]interface{}{"operations": operations})
	if err != nil {
		return studioFailure(err.Error())
	}
	return copilot.Result{Status: copilot.StatusOK, Data: screenData(reply)}
}

func (t *studioEditTool) operations(args map[string]interface{}) (any, error) {
	if t.studioScope.kind == copilot.StudioImage {
		var a studioImageEditArgs
		if err := decodeArgs(args, &a); err != nil {
			return nil, err
		}
		return a.Operations, checkOperations(a.Operations, t.allowed)
	}
	var a studioVideoEditArgs
	if err := decodeArgs(args, &a); err != nil {
		return nil, err
	}
	return a.Operations, checkOperations(a.Operations, t.allowed)
}

func checkOperations[T any](ops []T, allowed []string) error {
	if len(ops) == 0 || len(ops) > MaxStudioOperations {
		return fmt.Errorf("%w: envie de 1 a %d operações", errInvalidArgs, MaxStudioOperations)
	}
	for i, op := range ops {
		value := reflect.ValueOf(op)
		name := strings.TrimSpace(value.FieldByName("Op").String())
		if !contains(allowed, name) {
			return fmt.Errorf("%w: operação %d: op %q não existe; use uma de: %s", errInvalidArgs, i+1, name, strings.Join(allowed, ", "))
		}
		if keys := value.FieldByName("Keys"); keys.IsValid() && keys.Len() > maxStudioKeys {
			return fmt.Errorf("%w: operação %d: no máximo %d chaves por propriedade", errInvalidArgs, i+1, maxStudioKeys)
		}
		if easing, ok := unknownEasing(value.FieldByName("Keys")); ok {
			return fmt.Errorf("%w: operação %d: easing %q não existe; use %s ou cubic-bezier(x1,y1,x2,y2) com x de 0 a 1 e y de -1 a 2", errInvalidArgs, i+1, easing, easingNames())
		}
		if !finiteNumbers(value) {
			return fmt.Errorf("%w: operação %d: número inválido", errInvalidArgs, i+1)
		}
	}
	return nil
}

func unknownEasing(keys reflect.Value) (string, bool) {
	if !keys.IsValid() || keys.Kind() != reflect.Slice {
		return "", false
	}
	for i := 0; i < keys.Len(); i++ {
		easing := keys.Index(i).FieldByName("Easing").String()
		if easing != "" && !mediagen.Easing(easing).Known() {
			return easing, true
		}
	}
	return "", false
}

func easingNames() string {
	names := make([]string, 0, len(mediagen.Easings()))
	for _, e := range mediagen.Easings() {
		names = append(names, string(e))
	}
	return strings.Join(names, ", ")
}

func finiteNumbers(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Ptr:
		return v.IsNil() || finiteNumbers(v.Elem())
	case reflect.Float64:
		return !math.IsNaN(v.Float()) && !math.IsInf(v.Float(), 0)
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if !finiteNumbers(v.Field(i)) {
				return false
			}
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			if !finiteNumbers(v.Index(i)) {
				return false
			}
		}
	}
	return true
}

func (t *studioEditTool) NeedsApproval(mode copilot.Mode) bool { return mode == copilot.ModeAsk }

func (t *studioEditTool) Validate(ctx context.Context, cc copilot.Context, args map[string]interface{}) error {
	if _, err := t.operations(args); err != nil {
		return err
	}
	if _, err := t.deps.openProject(ctx, cc); err != nil {
		return fmt.Errorf("%w: %v", errInvalidArgs, err)
	}
	return nil
}

func (t *studioEditTool) Describe(_ context.Context, _ copilot.Context, args map[string]interface{}) []copilot.Field {
	ops, ok := args["operations"].([]interface{})
	if !ok {
		return nil
	}
	names := make([]string, 0, len(ops))
	for _, op := range ops {
		if fields, ok := op.(map[string]interface{}); ok {
			if name, ok := fields["op"].(string); ok {
				names = append(names, name)
			}
		}
	}
	return []copilot.Field{
		{Key: "operations", Value: strconv.Itoa(len(names))},
		{Key: "steps", Value: strings.Join(names, ", ")},
	}
}
