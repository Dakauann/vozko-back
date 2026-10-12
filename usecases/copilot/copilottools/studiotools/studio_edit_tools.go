package studiotools

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"sync"

	"vozko/domain/copilot"
	"vozko/domain/mediagen"
	"vozko/domain/studio"
	"vozko/domain/tools"
	"vozko/usecases/copilot/copilottools"
)

const (
	MaxStudioOperations = 40
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
	FontSize      *float64             `json:"font_size,omitempty" desc:"altura da letra como fração da altura do quadro (0.1 é um décimo da altura)"`
	FontWeight    *int                 `json:"font_weight,omitempty" desc:"peso de 100 a 900"`
	Align         string               `json:"align,omitempty" enum:"left,center,right" desc:"alinhamento do texto"`
	Shape         string               `json:"shape,omitempty" enum:"rect,ellipse,line,arrow,triangle,star,polygon,path" desc:"forma (add_shape); polygon é um polígono regular com points lados; path desenha o caminho vetorial de path"`
	Path          string               `json:"path,omitempty" desc:"caminho vetorial SVG (shape path no add_shape, update_layer de uma forma path, ou a curva custom de set_text_path, com coordenadas de 0 a 1 na caixa do clipe): comandos M L H V C S Q T A Z, números separados por espaço, coordenadas de 0 a 1 dentro da caixa da camada; ex.: M0 0.6 L1 0.4 L1 1 L0 1 Z"`
	PathPreset    string               `json:"path_preset,omitempty" enum:"diagonalBand,diagonalSplit,arch,ribbon,chevron,wave,blob,speechBubble,heart,swoosh,tornPaper,cornerBracket,squiggle,burst,seal" desc:"forma vetorial pronta (add_shape), no lugar de shape"`
	FillRule      string               `json:"fill_rule,omitempty" enum:"nonzero,evenodd" desc:"forma path: evenodd vaza as sobreposições (o miolo do O, um anel); nonzero, o padrão, preenche tudo"`
	LineCap       string               `json:"line_cap,omitempty" enum:"butt,round,square" desc:"ponta do traço"`
	LineJoin      string               `json:"line_join,omitempty" enum:"miter,round,bevel" desc:"junção dos cantos do traço"`
	MiterLimit    *float64             `json:"miter_limit,omitempty" desc:"limite do canto em bico quando line_join é miter, de 1 a 20"`
	DashArray     []float64            `json:"dash_array,omitempty" desc:"padrão do tracejado em múltiplos da espessura: [3,2] tracejado, [0,2] com line_cap round faz pontos; até 8 valores; [] volta ao traço contínuo"`
	DashOffset    *float64             `json:"dash_offset,omitempty" desc:"deslocamento do tracejado em múltiplos da espessura"`
	Points        *int                 `json:"points,omitempty" desc:"pontas da estrela ou lados do polígono, de 3 a 64"`
	Inner         *float64             `json:"inner,omitempty" desc:"raio interno da estrela como fração da ponta, de 0.05 a 1 (0.45 padrão)"`
	Italic        *bool                `json:"italic,omitempty" desc:"texto em itálico"`
	LineHeight    *float64             `json:"line_height,omitempty" desc:"altura da linha do texto, de 0.5 a 4 (1.2 normal)"`
	LetterSpacing *float64             `json:"letter_spacing,omitempty" desc:"espaçamento entre letras como fração do tamanho da letra, de -0.5 a 2"`
	Radius        *float64             `json:"radius,omitempty" desc:"arredondamento dos cantos de 0 a 1 (retângulos e imagens)"`
	Dash          *bool                `json:"dash,omitempty" desc:"contorno tracejado"`
	ArrowStart    *bool                `json:"arrow_start,omitempty" desc:"ponta de seta no início (linhas, setas e caminhos abertos)"`
	ArrowEnd      *bool                `json:"arrow_end,omitempty" desc:"ponta de seta no fim (linhas, setas e caminhos abertos)"`
	StartKind     string               `json:"arrow_start_kind,omitempty" enum:"arrow,openArrow,round,square,bar,dot,diamond" desc:"estilo da ponta do início e liga a ponta: arrow seta cheia, openArrow seta aberta, round arredondada, square quadrado, bar barra, dot bolinha, diamond losango"`
	EndKind       string               `json:"arrow_end_kind,omitempty" enum:"arrow,openArrow,round,square,bar,dot,diamond" desc:"estilo da ponta do fim e liga a ponta, com os mesmos valores de arrow_start_kind"`
	Shadow        *studioShadowArgs    `json:"shadow,omitempty" desc:"sombra; informe ao menos um campo; objeto vazio é ignorado"`
	Gradient      *studioGradientArgs  `json:"gradient,omitempty" desc:"degradê no texto ou na forma; trocar fill por uma cor sólida remove o degradê"`
	Highlight     *studioHighlightArgs `json:"highlight,omitempty" desc:"caixa colorida atrás do texto"`
	Curve         *float64             `json:"curve,omitempty" desc:"curva do texto em arco, de -1 a 1"`
	BlendMode     string               `json:"blend_mode,omitempty" enum:"normal,multiply,screen,overlay,darken,lighten,color-dodge,add,color-burn,hard-light,soft-light,difference,exclusion,hue,saturation,color,luminosity" desc:"mistura com o que está embaixo; em clipes de vídeo e imagem vale para o clipe inteiro (update_clip); add soma a luz (vazamentos de luz, brilhos e fogo sobre fundo preto), screen clareia mais suave, multiply escurece"`
}

type studioShadowArgs struct {
	Color string   `json:"color,omitempty" desc:"cor #rrggbbaa"`
	Blur  *float64 `json:"blur,omitempty" desc:"desfoque em pixels da arte, de 0 a 200"`
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
	AtMS    int64              `json:"at_ms" req:"true" desc:"instante da chave em ms, contado do início do clipe: de 0 até a duração do clipe"`
	Value   *float64           `json:"value,omitempty" desc:"para x, y, scale, scaleX, scaleY, rotation, opacity e blur: x e y de -1 a 2 (0.5 é o centro; fora de 0 a 1 sai da tela); scale, scaleX (só a largura) e scaleY (só a altura) de 0.05 a 5 (1 é o tamanho atual; o clipe animado, contando o quanto a curva passa da chave, não pode passar de 4 vezes o quadro); rotation de -3600 a 3600 graus; opacity de 0 a 1; blur de 0 a 100 px (0 é nítido); gainDb de -60 a 6 dB; pan de -1 (esquerda) a 1 (direita); counter de -1e12 a 1e12; pathOffset de -4 a 4; exposure de -5 a 5 stops; contrast, highlights, shadows, whites, blacks, temperature, tint, saturation e vibrance de -100 a 100; speed de 0.1 a 16 (sem passar disso entre as chaves)"`
	Color   string             `json:"color,omitempty" desc:"para fill e stroke: a cor da chave em #rrggbb ou #rrggbbaa; as cores se misturam entre as chaves"`
	X       *float64           `json:"x,omitempty" desc:"para position: centro horizontal do clipe nesta chave, de -1 a 2 (0.5 é o centro do quadro)"`
	Y       *float64           `json:"y,omitempty" desc:"para position: centro vertical do clipe nesta chave, de -1 a 2 (0.5 é o centro do quadro)"`
	Spatial string             `json:"spatial,omitempty" enum:"linear,auto,smooth,free" desc:"para position: forma do trajeto nesta chave; auto (padrão) curva sozinho e suave pelas chaves, linear faz uma quina reta, smooth usa as alças in e out alinhadas, free usa in e out independentes (quina curva)"`
	In      *studioTangentArgs `json:"in,omitempty" desc:"para position com spatial smooth ou free: alça que chega na chave, relativa a ela; obrigatória nesses dois modos e proibida nos outros"`
	Out     *studioTangentArgs `json:"out,omitempty" desc:"para position com spatial smooth ou free: alça que sai da chave, relativa a ela; em smooth aponte para o lado oposto de in"`
	Easing  string             `json:"easing,omitempty" desc:"curva do trecho que sai desta chave, padrão easeInOut: linear, hold, easeIn, easeOut, easeInOut, backIn (recua antes de sair), backOut (passa do ponto e volta), backInOut, elastic (vibra), bounce (quica), spring (chega com impulso de mola) ou uma curva css cubic-bezier(x1,y1,x2,y2) com x de 0 a 1 e y de -1 a 2, como cubic-bezier(0.05,0.7,0.1,1) para uma entrada enfática"`
	Smooth  *bool              `json:"smooth,omitempty" desc:"para propriedades com número: true deixa a velocidade contínua automática nesta chave; a curva dos dois lados é recalculada sempre que as chaves mudam"`
	studioMorphKeyArgs
}

type studioVideoOperation struct {
	Op         string          `json:"op" req:"true" enum:"add_media,add_text,add_shape,add_icon,add_element,add_captions,move_clip,trim_clip,slip_clip,roll_edit,slide_clip,split_clip,delete_clips,duplicate_clips,update_clip,animate,animate_effect,motion_preset,entrance,exit,animate_text,set_speed,freeze_frame,speed_ramp,set_speed_curve,set_color,set_chroma_key,animate_chroma,remove_chroma_key,set_luma_key,animate_luma,remove_luma_key,add_mask,update_mask,animate_mask,move_mask,remove_mask,add_effect_clip,set_camera,add_vfx,update_vfx,animate_vfx,move_vfx,remove_vfx,set_line,set_counter,remove_counter,set_text_path,remove_text_path,style_text,add_text_animator,update_text_animator,animate_text_animator,move_text_animator,remove_text_animator,add_track,move_track,update_track,remove_track,add_marker,update_marker,remove_marker,link_clips,unlink_clips,set_ducking,clear_ducking,bake_ducking,normalize_loudness,set_canvas,seek,select,add_transition,update_transition,remove_transition,set_anchor,scale_axes,set_path,ease_keys,smooth_keys,reverse_keys,time_stretch_keys,rove_keys,align_clips,distribute_clips,sequence_clips,stagger_clips,parent_clip,add_null,add_behavior,update_behavior,remove_behavior,bake_behavior,group_clips,ungroup_clips,track_point,attach_to_track,stabilize_clip,remove_tracker,set_path_ops,animate_path_op,set_repeat,clear_repeat,animate_repeat,add_chart,update_chart_data,animate_chart,mark_beats,apply_animation_preset,add_table,update_table,add_qr_code,update_qr_code,import_lottie" desc:"a operação"`
	Ref        string          `json:"ref,omitempty" desc:"nome curto para o clipe, faixa ou máscara que esta operação cria; as operações seguintes do mesmo lote usam @nome"`
	ClipID     string          `json:"clip_id,omitempty" desc:"clipe alvo: id de studio_read ou @ref"`
	ClipIDs    []string        `json:"clip_ids,omitempty" desc:"vários clipes (delete_clips, duplicate_clips, update_clip, motion_preset, entrance, exit, select, parent_clip, align_clips, distribute_clips, sequence_clips e stagger_clips na ordem pedida; add_element de moldura ou grade: os clipes recortados, na ordem dos espaços)"`
	TrackID    string          `json:"track_id,omitempty" desc:"faixa alvo ou de destino: id de studio_read ou @ref"`
	MarkerID   string          `json:"marker_id,omitempty" desc:"marcador alvo (update_marker, remove_marker): id de markers em studio_read, do vídeo ou de um clipe, ou @ref de add_marker"`
	MediaID    string          `json:"media_id,omitempty" desc:"mídia da biblioteca (add_media), o arquivo de legendas pronto (add_captions) ou o arquivo Lottie da biblioteca, de tipo lottie (import_lottie)"`
	IconID     string          `json:"icon_id,omitempty" desc:"ícone (add_icon): um id do catálogo, que studio_read full lista, ou o item_id de um ícone de studio_library_search; os outros itens da biblioteca entram com add_element"`
	AtMS       *int64          `json:"at_ms,omitempty" desc:"instante na linha do tempo em ms: onde inserir, cortar, marcar ou posicionar o cursor de tempo"`
	StartMS    *int64          `json:"start_ms,omitempty" desc:"novo início do clipe na linha do tempo (move_clip, trim_clip)"`
	EndMS      *int64          `json:"end_ms,omitempty" desc:"novo fim do clipe na linha do tempo (trim_clip)"`
	DurationMS *int64          `json:"duration_ms,omitempty" desc:"duração em ms (textos, formas, imagens)"`
	TrimInMS   *int64          `json:"trim_in_ms,omitempty" desc:"de que ponto do arquivo de origem o clipe começa, em ms (slip_clip)"`
	ToIndex    *int            `json:"to_index,omitempty" desc:"posição da faixa, 0 é a de baixo (move_track, add_track)"`
	Style      string          `json:"style,omitempty" enum:"headline,caption,cta,lowerThird,tag" desc:"estilo pronto de texto (add_text)"`
	Preset     string          `json:"preset,omitempty" enum:"kenBurns,enterLeft,slideUp,fadeIn,fadeOut,pulse,spin,wobble,pop,drop,springIn" desc:"animação pronta por keyframes (motion_preset); pop cresce passando do tamanho, drop cai e quica, springIn entra com mola"`
	Effect     string          `json:"effect,omitempty" enum:"none,fade,slideUp,slideDown,slideLeft,slideRight" desc:"efeito de entrada ou de saída (entrance, exit): fade ou deslize curto com fade; none tira o efeito e a transição desse lado; não vai junto com transition_kind"`
	Property   string          `json:"property,omitempty" enum:"x,y,scale,scaleX,scaleY,position,rotation,opacity,blur,fill,stroke,gainDb,pan,trimStart,trimEnd,trimOffset,waveAmount,waveLength,waveSpeed,wavePhase,wiggleSize,wiggleDetail,wiggleRate,taperStartLength,taperEndLength,taperStartWidth,taperEndWidth,taperStartEase,taperEndEase,counter,pathOffset,path,exposure,contrast,highlights,shadows,whites,blacks,temperature,tint,saturation,vibrance,speed,cameraPanX,cameraPanY,cameraZoom,cameraRotation,rotationX,rotationY" desc:"propriedade animada (animate); scaleX e scaleY esticam só a largura ou só a altura e não animam junto com scale; position é o trajeto curvo, com chaves de x e y, e não anima junto com x e y (animate troca as chaves rivais e avisa); blur é o desfoque em pixels do quadro; fill e stroke animam a cor de textos, formas e ícones (chaves com color); gainDb é o volume em dB de clipes de áudio, de -60 (silêncio) a 6, somado ao volume do clipe; pan é a posição do clipe de áudio no estéreo, de -1 (esquerda) a 1 (direita), 0 no centro; trim*, wave*, wiggle* e taper* animam o traço de linhas e formas com contorno, nas unidades e limites dos campos de set_line, e ligam o grupo com os padrões quando ele está desligado; counter é o número do contador e pathOffset o lugar do texto na sua curva, nos limites de set_counter e set_text_path; path muda o desenho de uma forma (chaves com path); animate liga o contador ou o texto em curva com os padrões quando estão desligados"`
	Keys       []studioKeyArgs `json:"keys,omitempty" desc:"chaves da animação (animate); substituem as chaves dessa propriedade; vazio remove a animação dela"`
	Volume     *float64        `json:"volume,omitempty" desc:"volume de 0 a 2 (1 normal)"`
	FadeInMS   *int64          `json:"fade_in_ms,omitempty" desc:"duração do fade de entrada em ms"`
	FadeOutMS  *int64          `json:"fade_out_ms,omitempty" desc:"duração do fade de saída em ms"`
	Fit        string          `json:"fit,omitempty" enum:"cover,contain" desc:"cover preenche o quadro cortando; contain mostra inteiro"`
	Kind       string          `json:"kind,omitempty" enum:"visual,audio" desc:"tipo da faixa (add_track)"`
	Name       string          `json:"name,omitempty" desc:"nome da faixa, rótulo do marcador, nome do clipe (update_clip, mostrado na linha do tempo) ou nome do grupo (group_clips)"`
	Hidden     *bool           `json:"hidden,omitempty" desc:"esconder a faixa"`
	Locked     *bool           `json:"locked,omitempty" desc:"travar a faixa"`
	Muted      *bool           `json:"muted,omitempty" desc:"silenciar a faixa"`
	Disabled   *bool           `json:"disabled,omitempty" desc:"desativar o clipe sem apagar"`
	Blur       *float64        `json:"blur,omitempty" desc:"desfoque fixo do clipe em pixels do quadro, de 0 a 100 (update_clip); para animar use animate com property blur"`
	Ripple     *bool           `json:"ripple,omitempty" desc:"delete_clips: puxar o que vem depois para fechar o buraco; set_speed: false não move os outros clipes (padrão true)"`
	Edge       string          `json:"edge,omitempty" enum:"start,end" desc:"roll_edit: end move o corte entre este clipe e o que encosta depois dele, start o corte entre o anterior e este"`
	Linked     *bool           `json:"linked,omitempty" desc:"roll_edit e slide_clip: false edita só este clipe e deixa o clipe ligado (o som do vídeo) onde está; padrão true, os ligados acompanham"`
	Aspect     string          `json:"aspect,omitempty" enum:"square,portrait,story,landscape" desc:"formato do vídeo (set_canvas)"`
	Background string          `json:"background,omitempty" desc:"cor de fundo do vídeo #rrggbb (set_canvas)"`
	Clear      []string        `json:"clear,omitempty" enum:"shadow,stroke,gradient,highlight,curve,blend_mode" desc:"efeitos a remover do texto ou da forma"`
	studioTransitionArgs
	studioTextAnimationArgs
	studioSpeedArgs
	studioSoundArgs
	studioColorArgs
	studioChromaArgs
	studioLumaArgs
	studioMaskArgs
	studioVfxArgs
	studioLineArgs
	studioAnchorArgs
	studioFrameArgs
	studioKeyEaseArgs
	studioBoxArgs
	studioStyleArgs
	studioCounterArgs
	studioTextPathArgs
	studioTextAnimatorArgs
	studioTextRunArgs
	studioParentArgs
	studioBehaviorArgs
	studioTrackArgs
	studioMotionBlurArgs
	studioCameraArgs
	studioTiltArgs
	studioElementArgs
	studioGroupArgs
	studioClipColorArgs
	studioSpeedCurveArgs
	studioPathOpListArgs
	studioPathOpKeyArgs
	studioClipRepeatOpArgs
	studioChartArgs
	studioChartMotionArgs
	studioTableArgs
	studioQRArgs
	studioOrganizeArgs
	studioPresetArgs
	studioClipStyleArgs
}

type studioImageOperation struct {
	Op           string             `json:"op" req:"true" enum:"add_text,add_shape,add_image,add_icon,add_element,update_layer,style_text,delete_layers,duplicate_layers,order_layers,align_layers,distribute_layers,group_layers,ungroup_layers,scaffold_layers,set_hidden,set_locked,replace_image,fill_frame,update_artboard,add_artboard,duplicate_artboard,delete_artboard,move_to_artboard,arrange_artboards,select,combine_shapes,set_color,set_chroma_key,remove_chroma_key,import_svg,repeat_layers,set_repeat,clear_repeat,expand_repeat,create_component,add_instance,set_overrides,reset_overrides,detach_instance,swap_component,create_style,update_style,apply_style,detach_style,delete_style,set_layout,remove_layout,set_layout_item,add_paint_layer,paint_area,crop_to_area,copy_area_to_layer,add_vfx,update_vfx,move_vfx,remove_vfx,set_path_ops,add_chart,update_chart_data,expand_chart,add_table,update_table,add_qr_code,update_qr_code" desc:"a operação"`
	Ref          string             `json:"ref,omitempty" desc:"nome curto para o que esta operação cria (camada, grupo ou prancheta); as operações seguintes do mesmo lote usam @nome; depois de duplicate_artboard, @nome/id_da_camada_original aponta para a cópia dessa camada"`
	LayerID      string             `json:"layer_id,omitempty" desc:"camada alvo: id de studio_read ou @ref"`
	ArtboardID   string             `json:"artboard_id,omitempty" desc:"prancheta: id de studio_read ou @ref; nas criações escolhe onde a camada nasce (sem ele, na prancheta ativa); obrigatório em delete_artboard e no destino de move_to_artboard"`
	LayerIDs     []string           `json:"layer_ids,omitempty" desc:"várias camadas (delete, duplicate, order, align, distribute, group, ungroup, scaffold, set_hidden, set_locked, select, update_layer, combine_shapes)"`
	MediaID      string             `json:"media_id,omitempty" desc:"imagem da biblioteca, inclusive anexos da conversa (add_image, replace_image, fill_frame)"`
	IconID       string             `json:"icon_id,omitempty" desc:"ícone (add_icon, set_overrides): um id do catálogo, que studio_read full lista; add_icon também aceita o item_id de um ícone de studio_library_search, e os outros itens da biblioteca entram com add_element"`
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
	Clear        []string           `json:"clear,omitempty" enum:"shadow,stroke,gradient,highlight,curve,crop,filters,frame,clip,blend_mode,pattern,gradient_map,fills,strokes,effects,mask" desc:"efeitos a remover; mask tira a máscara de pixels da foto"`
	Hidden       *bool              `json:"hidden,omitempty" desc:"esconder (set_hidden)"`
	Locked       *bool              `json:"locked,omitempty" desc:"travar (set_locked)"`
	Width        *int               `json:"width,omitempty" desc:"largura da prancheta em pixels, de 100 a 4096 (add_artboard, update_artboard, duplicate_artboard para a versão em outro formato)"`
	Height       *int               `json:"height,omitempty" desc:"altura da prancheta em pixels, de 100 a 4096 (add_artboard, update_artboard, duplicate_artboard)"`
	Background   string             `json:"background,omitempty" desc:"cor de fundo da prancheta #rrggbb ou transparent (add_artboard, update_artboard); remove o degradê do fundo"`
	Name         string             `json:"name,omitempty" desc:"nome da camada ou da prancheta (add_artboard, duplicate_artboard, update_artboard)"`
	Mode         string             `json:"mode,omitempty" enum:"union,subtract,intersect,exclude,flatten" desc:"combine_shapes: union une as formas, subtract tira as de cima da mais baixa, intersect deixa só a sobreposição, exclude tira a sobreposição, flatten junta tudo num caminho sem cortar"`
	SVG          string             `json:"svg,omitempty" desc:"import_svg: o código completo de um <svg>; cada forma vira uma camada de vetor editável, agrupadas"`
	GroupID      string             `json:"group_id,omitempty" desc:"grupo alvo (set_repeat, clear_repeat, expand_repeat): id de grupo de studio_read ou @ref de repeat_layers"`
	Repeat       *studioRepeatArgs  `json:"repeat,omitempty" desc:"repeat_layers e set_repeat: a repetição; em set_repeat os campos omitidos mantêm os valores atuais, e mode novo recomeça dos padrões"`
	ComponentID  string             `json:"component_id,omitempty" desc:"componente (add_instance, swap_component): id de uma prancheta com component true de studio_read, @ref de create_component ou o id de uma instância dele"`
	PartID       string             `json:"part_id,omitempty" desc:"set_overrides e reset_overrides: a parte da instância, o id da camada no componente (part_id em parts de studio_read)"`
	Reset        []string           `json:"reset,omitempty" enum:"text,fill,stroke,font_id,font_weight,italic,media_id,crop,icon_id,hidden" desc:"reset_overrides: campos a voltar ao valor do componente; sem reset, volta a parte inteira (ou a instância inteira sem part_id)"`
	Component    *bool              `json:"component,omitempty" desc:"update_artboard: true usa a prancheta como componente; false desanexa as instâncias dela"`
	StyleID      string             `json:"style_id,omitempty" desc:"estilo alvo (update_style, apply_style, delete_style): id de styles em studio_read ou @ref de create_style"`
	StyleKind    string             `json:"style_kind,omitempty" enum:"color,text,effect" desc:"create_style: color guarda uma cor ou degradê, text guarda fonte, peso, itálico, tamanho, altura da linha e espaçamento, effect guarda uma sombra"`
	Role         string             `json:"role,omitempty" enum:"heading,subheading,body" desc:"create_style e update_style de texto: para que serve o estilo"`
	Description  string             `json:"description,omitempty" desc:"create_style e update_style: nota curta sobre quando usar o estilo"`
	Slot         string             `json:"slot,omitempty" enum:"fill,stroke,text,effect,background" desc:"create_style (de onde copiar), apply_style e detach_style: o campo da camada (fill, stroke, text, effect) ou o fundo da prancheta (background, com artboard_id); padrão pelo tipo do estilo"`
	Link         *bool              `json:"link,omitempty" desc:"create_style com layer_id: false cria o estilo sem ligar a camada (padrão true)"`
	StyleValues  *studioStyleSet    `json:"style_values,omitempty" desc:"create_style e update_style: os valores do estilo; em update_style os campos omitidos ficam como estão"`
	StyleClear   []string           `json:"style_clear,omitempty" enum:"font_id,font_weight,italic,font_size_px,line_height,letter_spacing" desc:"update_style de texto: campos que o estilo deixa de controlar"`
	studioColorArgs
	studioChromaKeyArgs
	studioBoxArgs
	studioStyleArgs
	studioPaintListArgs
	studioPixelArgs
	studioLayoutArgs
	studioTextRunArgs
	studioElementArgs
	studioStillVfxArgs
	studioPathOpListArgs
	studioLiveMergeArgs
	studioChartArgs
	studioTableArgs
	studioQRArgs
	studioArtboardArgs
}

type studioTransitionArgs struct {
	ToClipID       string   `json:"to_clip_id,omitempty" desc:"add_transition: o clipe que entra; sem ele, o clipe que encosta logo depois de clip_id na mesma faixa"`
	TransitionID   string   `json:"transition_id,omitempty" desc:"transição alvo (update_transition, remove_transition): id de studio_read ou @ref"`
	TransitionKind string   `json:"transition_kind,omitempty" enum:"dissolve,dipToColor,push,slide,wipe,iris,clockWipe,split,zoom,whip,blurDissolve,gradientWipe,venetianBlinds,blockDissolve,radialWipe,constantPower,constantGain" desc:"tipo da transição: em faixas visuais, dissolve funde, dipToColor passa por uma cor, push empurra, slide desliza por cima, wipe varre, iris abre em forma, clockWipe varre como relógio, split abre do centro, zoom aproxima, whip passa rápido com desfoque, blurDissolve funde com desfoque, gradientWipe aparece na ordem de um mapa de tons, venetianBlinds abre em lâminas de persiana, blockDissolve aparece em blocos soltos, radialWipe abre como um leque para os dois lados; entrance e exit aceitam todos os visuais menos dissolve (para isso use effect fade) e mostram ou escondem o clipe contra o que está abaixo dele; em faixas de áudio, constantPower cruza os sons sem queda de volume no meio (padrão, 1000 ms) e constantGain cruza em linha reta, para sons parecidos que somariam demais"`
	Alignment      string   `json:"alignment,omitempty" enum:"center,start,end" desc:"onde a transição fica em relação ao corte: center (padrão), start começa no corte, end termina no corte; dipToColor, zoom e whip só center"`
	Easing         string   `json:"easing,omitempty" desc:"curva da transição, como nas chaves (linear, easeInOut, backOut, spring, cubic-bezier(x1,y1,x2,y2)...), menos hold"`
	Direction      string   `json:"direction,omitempty" enum:"left,right,up,down" desc:"push, slide e whip: para onde a imagem anda"`
	Moving         string   `json:"moving,omitempty" enum:"in,out" desc:"slide em add_transition: in o clipe novo entra por cima, out o antigo sai por cima; em entrance e exit o lado já decide e moving não vale"`
	Color          string   `json:"color,omitempty" desc:"dipToColor: a cor #rrggbb (preto padrão; branco faz um flash)"`
	Angle          *float64 `json:"angle,omitempty" desc:"wipe, venetianBlinds e gradientWipe com wipe_map linear: direção da borda em graus, 0 anda para a direita, 90 para baixo; clockWipe e radialWipe: onde começa, 0 é meio-dia"`
	IrisShape      string   `json:"iris_shape,omitempty" enum:"circle,box,diamond" desc:"iris: forma da abertura"`
	OriginX        *float64 `json:"origin_x,omitempty" desc:"iris, clockWipe, radialWipe, zoom e gradientWipe radial ou diamond: centro horizontal de 0 a 1 (do quadro no corte, do próprio clipe na entrada e na saída)"`
	OriginY        *float64 `json:"origin_y,omitempty" desc:"iris, clockWipe, radialWipe, zoom e gradientWipe radial ou diamond: centro vertical de 0 a 1 (do quadro no corte, do próprio clipe na entrada e na saída)"`
	Clockwise      *bool    `json:"clockwise,omitempty" desc:"clockWipe: sentido horário (padrão true)"`
	Orientation    string   `json:"orientation,omitempty" enum:"horizontal,vertical" desc:"split: a abertura vertical abre para os lados, a horizontal para cima e para baixo"`
	Reverse        *bool    `json:"reverse,omitempty" desc:"iris e split: true fecha o clipe que sai em vez de abrir o que entra; gradientWipe: true inverte o mapa, o claro aparece primeiro; set_speed: true toca o clipe de trás para frente"`
	Amount         *float64 `json:"amount,omitempty" desc:"zoom: quanto aproxima; whip e blurDissolve: quanto desfoca; de 0 a 1"`
	studioRevealArgs
}

type studioSoundArgs struct {
	GainDB          *float64 `json:"gain_db,omitempty" desc:"update_track: volume da faixa de áudio em dB, de -60 a 6 (0 tira o ajuste)"`
	Pan             *float64 `json:"pan,omitempty" desc:"update_clip (clipe de áudio) e update_track (faixa de áudio): posição no estéreo, de -1 (tudo à esquerda) a 1 (tudo à direita), 0 é o centro e tira o ajuste; o pan da faixa soma ao dos clipes; pan animado muda com animate property pan"`
	TargetLUFS      *float64 `json:"target_lufs,omitempty" desc:"normalize_loudness: loudness alvo em LUFS, de -40 a -5; -14 para redes sociais, -16 para podcast, -23 para TV"`
	TriggerTrackIDs []string `json:"trigger_track_ids,omitempty" desc:"set_ducking: faixas de áudio cuja fala abaixa esta faixa (ids de studio_read ou @ref)"`
	ReduceDB        *float64 `json:"reduce_db,omitempty" desc:"set_ducking: quanto a faixa abaixa durante a fala, de 1 a 40 dB (padrão 12)"`
	AttackMS        *int64   `json:"attack_ms,omitempty" desc:"set_ducking: tempo para abaixar antes da fala, de 0 a 2000 ms (padrão 250)"`
	ReleaseMS       *int64   `json:"release_ms,omitempty" desc:"set_ducking: tempo para voltar depois da fala, de 0 a 3000 ms (padrão 500)"`
	HoldMS          *int64   `json:"hold_ms,omitempty" desc:"set_ducking: pausas menores que isso seguem abaixadas, de 0 a 2000 ms (padrão 300)"`
	Sensitivity     *float64 `json:"sensitivity,omitempty" desc:"set_ducking: de 0 (só fala forte) a 1 (fala baixa também conta), padrão 0.5"`
}

type studioTextAnimationArgs struct {
	Side      string `json:"side,omitempty" enum:"in,out,both" desc:"animate_text: in anima a chegada do texto, out a saída antes do fim do clipe; ease_keys: in suaviza a chegada na chave, out a saída e both os dois lados"`
	TextStyle string `json:"text_style,omitempty" enum:"fade,rise,slide,pop,blur,typewriter,none" desc:"animate_text: fade surge, rise sobe para o lugar, slide entra pela esquerda, pop cresce passando do tamanho, blur sai do desfoque, typewriter aparece de uma vez como digitação; none tira a animação desse lado"`
	Unit      string `json:"unit,omitempty" enum:"char,word,line" desc:"animate_text: anima letra por letra, palavra por palavra (padrão) ou linha por linha; typewriter usa char por padrão; add_text_animator e update_text_animator: a unidade do animador (char padrão)"`
	StaggerMS *int64 `json:"stagger_ms,omitempty" desc:"animate_text: atraso entre uma letra, palavra ou linha e a seguinte, de 0 a 2000 ms; animate_chart: atraso entre uma série ou categoria e a seguinte, de 0 a 5000 ms"`
}

type studioSpeedArgs struct {
	Speed         *float64 `json:"speed,omitempty" desc:"set_speed: velocidade de 0.1 a 16 (1 normal, 0.5 câmera lenta, 2 o dobro); o vídeo e o áudio ligados mudam juntos"`
	KeepDuration  *bool    `json:"keep_duration,omitempty" desc:"set_speed: true mantém a duração do clipe e toca mais ou menos da mídia; sem ele, a duração muda e a mesma parte da mídia toca"`
	PreservePitch *bool    `json:"preserve_pitch,omitempty" desc:"set_speed: false deixa o som ficar agudo ou grave com a velocidade (padrão true, a voz não muda de tom)"`
	StretchKeys   *bool    `json:"stretch_keys,omitempty" desc:"set_speed: false deixa as chaves de animação onde estão; padrão true, elas acompanham a nova duração"`
	RampMS        *int64   `json:"ramp_ms,omitempty" desc:"speed_ramp: quanto tempo da linha do tempo a rampa leva, a partir de at_ms"`
	RampSteps     *int     `json:"ramp_steps,omitempty" desc:"speed_ramp: em quantos degraus a velocidade muda (padrão 5); rampas curtas usam menos"`
}

type studioGradeArgs struct {
	Exposure    *float64 `json:"exposure,omitempty" desc:"exposição em stops de luz, de -5 a 5 (1 dobra a luz)"`
	Contrast    *float64 `json:"contrast,omitempty" desc:"contraste de -100 a 100"`
	Highlights  *float64 `json:"highlights,omitempty" desc:"realces (tons claros) de -100 a 100"`
	Shadows     *float64 `json:"shadows,omitempty" desc:"sombras (tons escuros) de -100 a 100"`
	Whites      *float64 `json:"whites,omitempty" desc:"ponto de branco de -100 a 100"`
	Blacks      *float64 `json:"blacks,omitempty" desc:"ponto de preto de -100 a 100 (positivo desbota os pretos)"`
	Temperature *float64 `json:"temperature,omitempty" desc:"temperatura de -100 (frio, azul) a 100 (quente, âmbar)"`
	Tint        *float64 `json:"tint,omitempty" desc:"matiz de -100 (verde) a 100 (magenta)"`
	Saturation  *float64 `json:"saturation,omitempty" desc:"saturação de -100 (preto e branco) a 100 (o dobro)"`
	Vibrance    *float64 `json:"vibrance,omitempty" desc:"vibração de -100 a 100: realça mais as cores apagadas e poupa a pele"`
}

type studioColorArgs struct {
	ColorGrade    *studioGradeArgs  `json:"color_grade,omitempty" desc:"set_color: os ajustes a mudar; 0 tira o ajuste; os outros ficam como estão"`
	Look          string            `json:"look,omitempty" enum:"warm,cool,faded,mono,punchy,dramatic,soft,vintage" desc:"set_color: look pronto que troca os ajustes de luz e cor antes de color_grade (curvas, cor seletiva e LUT ficam): warm quente, cool frio, faded desbotado, mono preto e branco, punchy vibrante, dramatic dramático, soft suave, vintage"`
	LookIntensity *float64          `json:"look_intensity,omitempty" desc:"set_color: intensidade do look de 0 a 1 (padrão 1)"`
	ResetColor    *bool             `json:"reset_color,omitempty" desc:"set_color: true tira toda a correção de cor (com curvas, cor seletiva, LUT e mapa de gradiente) antes de aplicar o resto"`
	Curves        *studioCurvesArgs `json:"curves,omitempty" desc:"set_color: curvas de tom; cada curva mandada substitui a anterior daquele canal"`
	CurvesPreset  string            `json:"curves_preset,omitempty" enum:"contrast,soften,fade,brighten,darken" desc:"set_color: curva master pronta: contrast (S), soften (S invertido), fade (pretos lavados), brighten, darken"`
	HSL           *studioHSLArgs    `json:"hsl,omitempty" desc:"set_color: cor seletiva sobre uma faixa de matiz; campos omitidos mantêm os atuais"`
	LutMediaID    string            `json:"lut_media_id,omitempty" desc:"set_color: um LUT .cube da biblioteca (mídia do tipo lut) aplicado depois dos ajustes"`
	LutIntensity  *float64          `json:"lut_intensity,omitempty" desc:"set_color: quanto do LUT entra, de 0 a 1 (padrão 1)"`
	ClearColor    []string          `json:"clear_color,omitempty" enum:"curves,hsl,lut,gradient_map" desc:"set_color: partes da cor a tirar antes de aplicar o resto"`
}

type studioChromaKeyArgs struct {
	ChromaColor  string   `json:"chroma_color,omitempty" desc:"set_chroma_key: a cor do fundo a remover em #rrggbb (vence chroma_preset)"`
	ChromaPreset string   `json:"chroma_preset,omitempty" enum:"green,blue,auto" desc:"set_chroma_key: green para tela verde, blue para tela azul, auto detecta a cor dominante na borda do quadro (recusa se não houver fundo verde ou azul)"`
	Similarity   *float64 `json:"similarity,omitempty" desc:"set_chroma_key: o quanto de cor parecida com o fundo some, de 0.001 a 1 (padrão 0.4); mais alto remove mais"`
	Smoothness   *float64 `json:"smoothness,omitempty" desc:"set_chroma_key: a suavidade da borda do recorte, de 0.001 a 1 (padrão 0.08)"`
	Spill        *float64 `json:"spill,omitempty" desc:"set_chroma_key: tira o reflexo da cor do fundo nas bordas, de 0.001 a 1 (padrão 0.1); mais alto tira mais"`
}

type studioChromaArgs struct {
	studioChromaKeyArgs
	ChromaProperty string `json:"chroma_property,omitempty" enum:"similarity,smoothness,spill" desc:"animate_chroma: o valor do recorte que as keys animam"`
}

type studioLumaArgs struct {
	LumaMode      string   `json:"luma_mode,omitempty" enum:"dark,light" desc:"set_luma_key: dark remove o escuro (fundo preto de vazamentos de luz, fogo, fumaça e partículas), light remove o claro (fundo branco)"`
	LumaThreshold *float64 `json:"luma_threshold,omitempty" desc:"set_luma_key: quanto da ponta escura (dark) ou clara (light) some, de 0 a 1 (padrão 0.1); mais alto remove mais"`
	LumaSoftness  *float64 `json:"luma_softness,omitempty" desc:"set_luma_key: a suavidade da passagem entre o que some e o que fica, de 0.001 a 1 (padrão 0.1)"`
	LumaProperty  string   `json:"luma_property,omitempty" enum:"threshold,softness" desc:"animate_luma: o valor do recorte por brilho que as keys animam"`
}

type studioLineArgs struct {
	TrimStart        *float64 `json:"trim_start,omitempty" desc:"set_line: onde o traço visível começa, fração do caminho de 0 a 1"`
	TrimEnd          *float64 `json:"trim_end,omitempty" desc:"set_line: onde o traço visível termina, fração do caminho de 0 a 1; de 0 a 1 nas chaves desenha o traço"`
	TrimOffset       *float64 `json:"trim_offset,omitempty" desc:"set_line: desloca o trecho visível ao longo do caminho em graus, de -3600 a 3600; 360 é uma volta inteira"`
	TrimSequential   *bool    `json:"trim_sequential,omitempty" desc:"set_line: true corta todos os traços do caminho como um só, um depois do outro; false corta cada traço igual"`
	WaveMode         string   `json:"wave_mode,omitempty" enum:"bend,width" desc:"set_line: bend ondula a linha, width faz a espessura pulsar"`
	WaveAmount       *float64 `json:"wave_amount,omitempty" desc:"set_line: altura da onda em pixels do quadro, de 0 a 500"`
	WaveLength       *float64 `json:"wave_length,omitempty" desc:"set_line: comprimento de uma onda em pixels do quadro, de 8 a 4000; em formas fechadas arredonda para um número inteiro de ondas"`
	WaveSpeed        *float64 `json:"wave_speed,omitempty" desc:"set_line: ondas por segundo que correm ao longo do traço, de -20 a 20; negativo corre para o início; 0 deixa a onda parada"`
	WavePhase        *float64 `json:"wave_phase,omitempty" desc:"set_line: desloca a onda em graus, de -3600 a 3600"`
	WiggleSize       *float64 `json:"wiggle_size,omitempty" desc:"set_line: quanto o traço treme em pixels do quadro, de 0 a 500"`
	WiggleDetail     *float64 `json:"wiggle_detail,omitempty" desc:"set_line: ondulações do tremor por 100 pixels de traço, de 0.05 a 10"`
	WiggleRate       *float64 `json:"wiggle_rate,omitempty" desc:"set_line: tremores por segundo, de 0 a 30; 0 deixa o tremor parado"`
	WiggleSeed       *int     `json:"wiggle_seed,omitempty" desc:"set_line: inteiro de 0 a 9999 que escolhe o desenho do tremor"`
	TaperStartLength *float64 `json:"taper_start_length,omitempty" desc:"set_line: comprimento da ponta afinada do início, fração do caminho de 0 a 1"`
	TaperEndLength   *float64 `json:"taper_end_length,omitempty" desc:"set_line: comprimento da ponta afinada do fim, fração do caminho de 0 a 1"`
	TaperStartWidth  *float64 `json:"taper_start_width,omitempty" desc:"set_line: espessura na ponta do início, fração da espessura do traço de 0 a 1"`
	TaperEndWidth    *float64 `json:"taper_end_width,omitempty" desc:"set_line: espessura na ponta do fim, fração da espessura do traço de 0 a 1"`
	TaperStartEase   *float64 `json:"taper_start_ease,omitempty" desc:"set_line: forma da ponta do início, de -1 (fina e longa) a 1 (arredondada); 0 é reta"`
	TaperEndEase     *float64 `json:"taper_end_ease,omitempty" desc:"set_line: forma da ponta do fim, de -1 (fina e longa) a 1 (arredondada); 0 é reta"`
	LineClear        []string `json:"line_clear,omitempty" enum:"trim,wave,wiggle,taper" desc:"set_line: grupos a desligar, junto com as chaves deles"`
}

type studioMaskArgs struct {
	MaskID       string   `json:"mask_id,omitempty" desc:"máscara alvo (update_mask, animate_mask, move_mask, remove_mask): id de studio_read ou @ref de add_mask"`
	MaskShape    string   `json:"mask_shape,omitempty" enum:"rectangle,ellipse,linear,mirror,star,heart,triangle,path" desc:"add_mask e update_mask: forma da máscara; linear mostra um lado de uma borda reta (rotation gira a borda), mirror mostra uma faixa de altura h; path usa path com coordenadas de 0 a 1 na caixa da máscara"`
	MaskMode     string   `json:"mask_mode,omitempty" enum:"add,subtract,intersect" desc:"add_mask e update_mask: add soma a área à imagem visível (padrão), subtract recorta a área, intersect mantém só onde esta e as anteriores se cruzam; as máscaras combinam em ordem"`
	MaskProperty string   `json:"mask_property,omitempty" enum:"x,y,w,h,rotation,roundness,inner,feather,expansion,opacity,path" desc:"animate_mask: propriedade da máscara que as keys animam; path muda o desenho de uma máscara path (chaves com path)"`
	Invert       *bool    `json:"invert,omitempty" desc:"add_mask e update_mask: true inverte a área desta máscara"`
	Roundness    *float64 `json:"roundness,omitempty" desc:"add_mask e update_mask: cantos arredondados do rectangle, de 0 a 1"`
	Feather      *float64 `json:"feather,omitempty" desc:"add_mask e update_mask: borda suave como fração do lado menor do clipe, de 0 a 0.5"`
	Expansion    *float64 `json:"expansion,omitempty" desc:"add_mask e update_mask: aumenta (positivo) ou encolhe (negativo) a área, de -0.5 a 0.5"`
	Matte        *bool    `json:"matte,omitempty" desc:"update_clip: true faz o clipe aparecer só onde o clipe visível logo abaixo tem imagem (texto com vídeo dentro, recorte por forma); false desliga"`
	MatteChannel string   `json:"matte_channel,omitempty" enum:"alpha,luma" desc:"update_clip: de onde o recorte sai; alpha (padrão) usa onde o clipe de baixo tem imagem, luma usa o brilho dele (claro mostra, escuro esconde); liga o recorte"`
	MatteInvert  *bool    `json:"matte_invert,omitempty" desc:"update_clip: true inverte o recorte: o clipe aparece onde o de baixo não tem imagem (alpha) ou onde ele é escuro (luma); liga o recorte"`
}

type studioVideoEditArgs struct {
	Operations []studioVideoOperation `json:"operations" req:"true" desc:"de 1 a 40 operações (prefira 8 a 12 por lote), aplicadas em ordem e juntas: se uma falhar, nenhuma é aplicada"`
}

type studioImageEditArgs struct {
	Operations []studioImageOperation `json:"operations" req:"true" desc:"de 1 a 40 operações (prefira 8 a 12 por lote), aplicadas em ordem e juntas: se uma falhar, nenhuma é aplicada"`
}

type studioEditTool struct {
	studioScope
	deps     StudioDeps
	name     string
	guide    string
	intro    string
	outro    string
	families []studioOpFamily
	core     []string
	args     any
	allowed  []string
	compact  func() tools.Definition
}

var (
	studioVideoCoreFields = []string{"op", "ref", "clip_id", "clip_ids", "track_id", "at_ms"}
	studioImageCoreFields = []string{"op", "ref", "layer_id", "layer_ids", "artboard_id"}
)

func NewStudioEditVideoTool(deps StudioDeps) copilot.Tool {
	return newStudioEditTool(&studioEditTool{studioScope: studioScope{kind: copilot.StudioVideo}, deps: deps, name: "studio_edit_video", guide: studioVideoEditGuide, intro: studioVideoEditIntro, outro: studioVideoEditOutro, families: studioVideoFamilies, core: studioVideoCoreFields, args: studioVideoEditArgs{}, allowed: opEnum(studioVideoOperation{})})
}

func NewStudioEditImageTool(deps StudioDeps) copilot.Tool {
	return newStudioEditTool(&studioEditTool{studioScope: studioScope{kind: copilot.StudioImage}, deps: deps, name: "studio_edit_image", guide: studioImageEditGuide, intro: studioImageEditIntro, outro: studioImageEditOutro, families: studioImageFamilies, core: studioImageCoreFields, args: studioImageEditArgs{}, allowed: opEnum(studioImageOperation{})})
}

func newStudioEditTool(t *studioEditTool) *studioEditTool {
	t.compact = sync.OnceValue(func() tools.Definition {
		return compactEditDefinition(t.Definition(), t.intro, t.outro, t.families, t.core)
	})
	return t
}

func opEnum(op any) []string {
	field, _ := reflect.TypeOf(op).FieldByName("Op")
	return copilottools.EnumValues(field)
}

func (t *studioEditTool) Meta() copilot.Meta { return studioEditMeta() }

func (t *studioEditTool) LogsArguments() bool { return true }

func (t *studioEditTool) Definition() tools.Definition {
	def := copilottools.Definition(t.name, t.guide, t.args)
	ops := def.Parameters["operations"]
	if ops.Items != nil {
		if font, ok := ops.Items.Properties["font_id"]; ok {
			font.Enum = studio.OrderedFonts()
			font.Description = fontGuide()
			ops.Items.Properties["font_id"] = font
		}
		describeShadow(ops.Items.Properties)
	}
	def.Parameters["operations"] = ops
	return def
}

func (t *studioEditTool) DefinitionOn(copilot.View) tools.Definition { return t.compact() }

func (t *studioEditTool) operationOrder() []string {
	var op reflect.Type
	if t.studioScope.kind == copilot.StudioImage {
		op = reflect.TypeOf(studioImageOperation{})
	} else {
		op = reflect.TypeOf(studioVideoOperation{})
	}
	var names []string
	for _, f := range reflect.VisibleFields(op) {
		if name := copilottools.JSONName(f); name != "" {
			names = append(names, name)
		}
	}
	return names
}

var fontCategoryNames = map[string]string{
	studio.FontSans:        "sem serifa",
	studio.FontSerif:       "serifadas",
	studio.FontDisplay:     "de destaque",
	studio.FontHandwriting: "manuscritas",
	studio.FontMono:        "monoespaçadas",
}

func describeShadow(props map[string]tools.Parameter) {
	shadow, ok := props["shadow"]
	if !ok || shadow.Items == nil {
		return
	}
	base := studio.DefaultShadow
	shadow.Description += fmt.Sprintf("; os campos omitidos usam a sombra padrão (%s, blur %g, y %g)", base.Color, base.Blur, base.Y)
	text := studio.TextShadow
	notes := map[string]string{
		"color": "em texto, um tom escuro translúcido como " + text.Color,
		"blur":  fmt.Sprintf("em texto, perto de %g por cento do tamanho da letra", text.BlurShare*100),
		"y":     fmt.Sprintf("em texto, perto de %g por cento do tamanho da letra", text.OffsetShare*100),
	}
	for name, note := range notes {
		if field, ok := shadow.Items.Properties[name]; ok {
			field.Description += "; " + note
			shadow.Items.Properties[name] = field
		}
	}
	props["shadow"] = shadow
}

func fontGuide() string {
	groups := make([]string, 0, len(studio.FontCategories))
	for _, category := range studio.FontCategories {
		groups = append(groups, fontCategoryNames[category]+": "+strings.Join(studio.FontsIn(category), ", "))
	}
	return "fonte; " + strings.Join(groups, "; ")
}

func (t *studioEditTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	operations, err := t.operations(args)
	if err != nil {
		return studioFailure(err.Error())
	}
	if _, err := t.deps.openProject(ctx, cc); err != nil {
		return studioFailure(err.Error())
	}
	payload, created, err := t.editorPayload(ctx, cc, operations, true)
	if err != nil {
		return studioFailure(err.Error())
	}
	reply, err := runOnEditor(ctx, cc, copilot.ScreenEdit, payload)
	if err != nil {
		if editorDidNotApply(err) {
			t.deps.Links.discard(ctx, cc, created)
		}
		return screenResult(cc, err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: screenData(reply)}
}

func (t *studioEditTool) operations(args map[string]interface{}) (any, error) {
	if t.studioScope.kind == copilot.StudioImage {
		var a studioImageEditArgs
		if err := copilottools.DecodeArgs(args, &a); err != nil {
			return nil, err
		}
		return a.Operations, checkBatch(a.Operations, t.allowed, t.studioScope.kind)
	}
	var a studioVideoEditArgs
	if err := copilottools.DecodeArgs(args, &a); err != nil {
		return nil, err
	}
	return a.Operations, checkBatch(a.Operations, t.allowed, t.studioScope.kind)
}

func checkBatch[T elementOperation](ops []T, allowed []string, editor copilot.StudioKind) error {
	if err := checkOperations(ops, allowed); err != nil {
		return err
	}
	return checkElements(ops, editor)
}

func checkOperations[T any](ops []T, allowed []string) error {
	if len(ops) == 0 || len(ops) > MaxStudioOperations {
		return fmt.Errorf("%w: envie de 1 a %d operações", copilottools.ErrInvalidArgs, MaxStudioOperations)
	}
	for i, op := range ops {
		value := reflect.ValueOf(op)
		name := strings.TrimSpace(value.FieldByName("Op").String())
		if !copilottools.Contains(allowed, name) {
			return fmt.Errorf("%w: operação %d: op %q não existe; use uma de: %s", copilottools.ErrInvalidArgs, i+1, name, strings.Join(allowed, ", "))
		}
		if msg := operationIssue(value); msg != "" {
			return fmt.Errorf("%w: operação %d: %s", copilottools.ErrInvalidArgs, i+1, msg)
		}
		if easing := value.FieldByName("Easing"); easing.IsValid() && easing.String() != "" && !mediagen.Easing(easing.String()).Known() {
			return fmt.Errorf("%w: operação %d: easing %q não existe; use %s ou cubic-bezier(x1,y1,x2,y2) com x de 0 a 1 e y de -1 a 2", copilottools.ErrInvalidArgs, i+1, easing.String(), easingNames())
		}
		if easing, ok := unknownEasing(value.FieldByName("Keys")); ok {
			return fmt.Errorf("%w: operação %d: easing %q não existe; use %s ou cubic-bezier(x1,y1,x2,y2) com x de 0 a 1 e y de -1 a 2", copilottools.ErrInvalidArgs, i+1, easing, easingNames())
		}
		if !finiteNumbers(value) {
			return fmt.Errorf("%w: operação %d: número inválido", copilottools.ErrInvalidArgs, i+1)
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
	operations, err := t.operations(args)
	if err != nil {
		return err
	}
	if _, err := t.deps.openProject(ctx, cc); err != nil {
		return fmt.Errorf("%w: %v", copilottools.ErrInvalidArgs, err)
	}
	if _, _, err := t.editorPayload(ctx, cc, operations, false); err != nil {
		return fmt.Errorf("%w: %v", copilottools.ErrInvalidArgs, err)
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
