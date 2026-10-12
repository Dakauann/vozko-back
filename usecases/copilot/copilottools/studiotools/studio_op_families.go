package studiotools

import (
	"reflect"
	"regexp"
	"strings"

	"vozko/domain/copilot"
	"vozko/usecases/copilot/copilottools"
)

var (
	studioShapeGroups      = []any{studioBoxArgs{}, studioStyleArgs{}}
	studioTransitionGroups = []any{studioTransitionArgs{}}
	studioQRGroups         = []any{studioQRArgs{}}
)

type studioOpFamily struct {
	name   string
	ops    []string
	guide  string
	groups []any
}

const studioVideoEditIntro = "Edita o projeto de vídeo aberto como uma pessoa faria no editor, com o seu cursor visível, em um único passo que o usuário desfaz com Ctrl+Z. "

const studioEditOutcome = " Devolve applied com o número de cada operação que entrou; numa recusa nada entra e o erro traz refused, com o motivo de cada operação recusada, e not_applied. Uma ref vale só no lote que a cria: created traz os ids de cada ref, e os lotes seguintes usam esses ids."

const studioVideoEditOutro = "Em cada operação, mande só os campos que ela usa. " +
	"Devolve os ids criados, os clipes alterados, os campos ignorados por não valerem para o clipe (ignored) e a checagem automática de design." + studioEditOutcome

const studioImageEditIntro = "Edita o projeto de imagem aberto como uma pessoa faria no editor, com o seu cursor visível, em um único passo que o usuário desfaz com Ctrl+Z. " +
	"As camadas vão de baixo para cima: a primeira é o fundo e a última fica na frente de todas (studio_read mostra z). "

const studioImageEditOutro = "Em cada operação, mande só os campos que ela usa. Devolve os ids criados, as camadas alteradas, os campos ignorados por não valerem para a camada (ignored) e a checagem automática de design." + studioEditOutcome

var studioVideoFamilies = []studioOpFamily{
	{name: "criar", groups: studioShapeGroups, ops: []string{"add_media", "add_text", "add_shape", "add_icon", "add_captions", "import_lottie"}, guide: "add_media(media_id, at_ms, track_id?, duration_ms?: vídeo traz o áudio junto), " +
		"add_text(text, at_ms, duration_ms?, track_id?, style?, x?, y?, w?, h?, fill?, gradient?, stroke?, font_id?, font_size?, font_weight?, italic?, align?, line_height?, letter_spacing?, highlight?, curve?, shadow?, blend_mode?), " +
		"add_shape(shape ou path_preset, path? quando shape é path, points? e inner? na estrela, at_ms, duration_ms?, track_id?, x, y, w, h, fill? ou gradient?, stroke?, stroke_width?, radius?, dash?, shadow?, blend_mode?), " +
		"add_icon(icon_id, at_ms, duration_ms?, track_id?, x?, y?, w?, h?, fill?), add_captions(media_id do arquivo .vtt, clip_id do clipe falado), " +
		"import_lottie(media_id de uma mídia de tipo lottie, at_ms?, track_id?): a animação vira um grupo de clipes comuns e editáveis (formas, contornos, chaves com as mesmas curvas, cortes de traçado, máscaras e pré-composições como grupos internos), no tamanho do quadro e centralizada; se o arquivo usa algo que não vira clipe (expressões, efeitos, imagens, textos, degradês, máscaras de trilha), nada entra e a resposta lista o que impede. "},
	{name: "linha-do-tempo", groups: studioShapeGroups, ops: []string{"move_clip", "trim_clip", "slip_clip", "roll_edit", "slide_clip", "split_clip", "delete_clips", "duplicate_clips", "update_clip"}, guide: "move_clip(clip_id, start_ms, track_id?), trim_clip(clip_id, start_ms? e ou end_ms?), slip_clip(clip_id, trim_in_ms), " +
		"roll_edit(clip_id, edge, at_ms, linked?: move o corte entre dois clipes que se encostam para at_ms; um encurta e o outro cresce, nada mais anda), slide_clip(clip_id, start_ms, linked?: anda o clipe entre os vizinhos sem mudar o conteúdo dele; o anterior e o seguinte se ajustam), split_clip(clip_id, at_ms), " +
		"delete_clips(clip_ids, ripple?), duplicate_clips(clip_ids), update_clip(clip_id ou clip_ids, volume?, pan?, fade_in_ms?, fade_out_ms?, fit?, disabled?, blur?, matte?, matte_channel?, matte_invert?, flip_x?, flip_y?, auto_orient?, motion_blur?, x?, y?, w?, h?, rotation?, opacity?, text? e estilo: italic, line_height, letter_spacing, radius, dash, arrow_start, arrow_end, arrow_start_kind, arrow_end_kind, shadow, gradient, highlight, curve, blend_mode, clear). "},
	{name: "animacao", groups: studioTransitionGroups, ops: []string{"animate", "motion_preset", "entrance", "exit"}, guide: "animate(clip_id, property, keys), motion_preset(clip_id ou clip_ids, preset), entrance(clip_id ou clip_ids, effect? ou transition_kind?, duration_ms?, easing? e as opções do tipo), exit(os mesmos campos): com transition_kind o clipe aparece ou some com qualquer transição visual menos dissolve, medida na caixa do próprio clipe e contra o que está abaixo dele (um wipe revela um logo sobre o vídeo, um zoom cresce de dentro dele); gradientWipe usa wipe_map ou wipe_map_media_id, softness e reverse, venetianBlinds e blockDissolve usam piece_size e softness, radialWipe usa angle, origin_x, origin_y e softness; trocar de lado com effect apaga a transição dele e vice-versa; o lado de um clipe que já tem transição de corte com o vizinho não aceita entrada ou saída própria. " +
		studioMorphGuide +
		studioMotionBlurGuide +
		studioTiltGuide +
		"Formato de um lote com ref e chaves (os valores só mostram a forma dos campos; efeito, tempos, propriedade e posição vêm do conceito de movimento da peça): [{\"op\":\"add_text\",\"ref\":\"t1\",\"text\":\"TEXTO\",\"at_ms\":0,\"duration_ms\":2400,\"x\":0.3,\"y\":0.72}," +
		"{\"op\":\"entrance\",\"clip_id\":\"@t1\",\"effect\":\"slideRight\",\"duration_ms\":380},{\"op\":\"animate\",\"clip_id\":\"@t1\",\"property\":\"x\"," +
		"\"keys\":[{\"at_ms\":380,\"value\":0.3},{\"at_ms\":2000,\"value\":0.36}]}]. "},
	{name: "trajeto", ops: []string{"set_anchor", "scale_axes", "set_path"}, guide: "Âncora, escala por eixo e trajeto curvo em clipes com imagem: set_anchor(clip_id ou clip_ids, anchor? ou anchor_x? e anchor_y?, keep_visual?: o ponto em volta do qual o clipe gira e cresce; x e y continuam sendo o centro da caixa), " +
		"scale_axes(clip_id ou clip_ids, separate, keep_axis?: separa scale em scaleX e scaleY ou junta de volta), set_path(clip_id ou clip_ids, curved: junta x e y num trajeto curvo position ou separa de volta); " +
		"animate com property position recebe chaves com x, y, spatial? e as alças in e out (smooth e free): a curva da chave controla a velocidade ao longo do trajeto; update_clip com auto_orient faz o clipe apontar para onde anda e flip_x e flip_y espelham a imagem sem mover o clipe. "},
	{name: "chaves", ops: []string{"ease_keys", "smooth_keys", "reverse_keys", "time_stretch_keys", "rove_keys"}, guide: "Chegada e saída das chaves (o easing de uma chave é o trecho que sai dela; cada chave tem um lado de chegada e um de saída): ease_keys(clip_id, property?, keys_ms?, side, key_speed?, influence?: side in suaviza a chegada na chave, out a saída e both os dois; sem key_speed nem influence a chave para com 33.3 por cento de influência, a suavização padrão; key_speed é a velocidade naquele lado por segundo, na unidade dos valores, e influence é quanto do trecho a curva usa para chegar nela), " +
		"smooth_keys(clip_id, property?, keys_ms?, smooth?: velocidade contínua automática pela chave, recalculada quando as chaves mudam), reverse_keys(clip_id, property?, keys_ms?: toca as chaves de trás para frente no mesmo trecho), time_stretch_keys(clip_id, property?, keys_ms?, percent, from_key?: muda o tempo entre as chaves sem mudar os valores), rove_keys(clip_id, property?, keys_ms?: as chaves do meio andam no tempo para a velocidade ficar constante); animate também aceita smooth em cada chave. studio_read full traz ease_in e ease_out de cada chave (influence em por cento e speed por segundo nas propriedades com número e em position, onde a velocidade é medida pelo caminho em alturas do quadro por segundo), peak_speed (a maior velocidade do trecho que sai da chave, para conferir o ritmo) e smooth nas chaves automáticas. "},
	{name: "velocidade", ops: []string{"set_speed", "freeze_frame", "speed_ramp", "set_speed_curve"}, guide: "set_speed(clip_id de vídeo ou áudio, speed? ou duration_ms? para caber nessa duração, keep_duration?, reverse?, preserve_pitch?, ripple? padrão true puxa ou empurra o resto da faixa, stretch_keys?), freeze_frame(clip_id de vídeo, at_ms? padrão o cursor de tempo, duration_ms? padrão 2000: congela o quadro desse instante num clipe novo e empurra o resto), " +
		"speed_ramp(clip_id de vídeo ou áudio, speed de chegada, ramp_ms, at_ms? padrão o cursor de tempo, ramp_mode? padrão smooth, ramp_steps? só em steps, easing? padrão easeInOut: a velocidade atual vira speed aos poucos e o resto do clipe segue em speed; smooth faz uma curva no mesmo clipe, steps divide o clipe em degraus), " +
		studioSpeedCurveGuide},
	{name: "cor", ops: []string{"set_color"}, guide: "set_color(clip_id ou clip_ids de vídeo ou imagem, color_grade? com exposure, contrast, highlights, shadows, whites, blacks, temperature, tint, saturation, vibrance, look?, look_intensity?, curves?, curves_preset?, hsl?, lut_media_id?, lut_intensity?, gradient_map?, clear_color?, reset_color?), " +
		studioColorGuide +
		studioVideoColorGuide},
	{name: "recortes", ops: []string{"set_chroma_key", "animate_chroma", "remove_chroma_key", "add_mask", "update_mask", "animate_mask", "move_mask", "remove_mask", "set_luma_key", "animate_luma", "remove_luma_key"}, guide: "Recorte de fundo (tela verde ou azul) em clipes de vídeo e imagem: set_chroma_key(clip_id, chroma_preset? green, blue ou auto (detecta a cor na borda do quadro em at_ms, ou no quadro do playhead quando ele está no clipe, ou no primeiro quadro) ou chroma_color?, similarity?, smoothness?, spill?, at_ms? em ms dentro do clipe para gravar uma chave quando o valor já é animado: liga o recorte com os padrões e ajusta), animate_chroma(clip_id, chroma_property, keys com at_ms dentro do clipe), remove_chroma_key(clip_id); o que fica transparente mostra as faixas de baixo. " +
		"Máscaras recortam um clipe visual em coordenadas de 0 a 1 do quadro do clipe: add_mask(clip_id, mask_shape, mask_mode?, invert?, x?, y?, w?, h?, rotation?, roundness?, points? e inner? na star, path? na path, feather?, expansion?, opacity?, to_index?), " +
		"update_mask(clip_id, mask_id, os mesmos campos, at_ms? em ms dentro do clipe para gravar uma chave quando a propriedade já é animada), animate_mask(clip_id, mask_id, mask_property, keys com at_ms dentro do clipe: a máscara aparece, anda ou cresce só a partir de um momento), move_mask(clip_id, mask_id, to_index), remove_mask(clip_id, mask_id); update_clip com matte recorta o clipe pelo clipe de baixo, matte_channel luma recorta pelo brilho dele e matte_invert inverte. " +
		"Recorte por brilho em clipes de vídeo e imagem: set_luma_key(clip_id, luma_mode? dark (padrão) ou light, luma_threshold?, luma_softness?, at_ms? em ms dentro do clipe para gravar uma chave quando o valor já é animado), animate_luma(clip_id, luma_property, keys com at_ms dentro do clipe), remove_luma_key(clip_id); vem depois do recorte de fundo e antes da cor. "},
	{name: "efeitos", ops: []string{"add_effect_clip", "add_vfx", "update_vfx", "animate_vfx", "move_vfx", "remove_vfx", "set_camera"}, guide: "Efeitos visuais (procedurais, editáveis e iguais na prévia e na exportação): add_effect_clip(vfx_kind?, at_ms?, duration_ms?, track_id?, vfx_params?, vfx_seed?: um clipe de efeito que vale para tudo o que aparece abaixo dele durante o seu tempo; sem track_id fica acima de tudo o que está na tela; fade_in_ms, fade_out_ms e opacity em update_clip dosam o efeito; move_clip, trim_clip e delete_clips valem para ele), " +
		"add_vfx(clip_id, vfx_kind, vfx_params?, vfx_seed?, vfx_track_id? nos efeitos de som, to_index?: o efeito só dentro desse clipe; shake num clipe move o próprio clipe, posterizeTime trava o clipe em menos quadros por segundo e echo desenha cópias do clipe em instantes anteriores atrás dele), update_vfx(clip_id, vfx_id?, vfx_params?, vfx_seed?, vfx_hidden?, vfx_track_id?, at_ms? em ms dentro do clipe para gravar uma chave quando o valor já é animado), " +
		"Num clipe de efeito, posterizeTime trava tudo o que está abaixo; fractalNoise, gradientRamp e fourColorGradient num clipe de efeito na faixa de baixo viram um fundo animado; audioSpectrum e audioWaveform desenham barras que seguem a faixa de áudio de vfx_track_id e a exportação recusa se o som não puder ser lido. " +
		"animate_vfx(clip_id, vfx_id?, vfx_property, keys com at_ms dentro do clipe), move_vfx(clip_id, vfx_id?, to_index), remove_vfx(clip_id, vfx_id?); os efeitos de um clipe se aplicam em ordem, de cima para baixo da lista. " +
		studioAdjustmentGuide},
	{name: "traco", ops: []string{"set_line"}, guide: "Traço de linhas, setas e formas com contorno (stroke e stroke_width, sem shadow nem effects): set_line(clip_id, at_ms? em ms dentro do clipe para gravar uma chave quando o valor já é animado, trim_start?, trim_end?, trim_offset?, trim_sequential?, wave_mode?, wave_amount?, wave_length?, wave_speed?, wave_phase?, wiggle_size?, wiggle_detail?, wiggle_rate?, wiggle_seed?, taper_start_length?, taper_end_length?, taper_start_width?, taper_end_width?, taper_start_ease?, taper_end_ease?, line_clear?: cada campo liga o grupo dele com os padrões e muda só o que veio; o preenchimento da forma fica parado); animate com property trimEnd, waveAmount, wiggleSize e as outras do traço muda o valor ao longo do tempo com as curvas das chaves. "},
	{name: "texto", ops: []string{"set_counter", "remove_counter", "set_text_path", "remove_text_path", "style_text", "animate_text", "add_text_animator", "update_text_animator", "animate_text_animator", "move_text_animator", "remove_text_animator"}, guide: "Contador animado em textos: set_counter(clip_id de um texto, counter_value? ou counter_from e counter_to com count_start_ms?, count_ms? e easing? padrão easeOut, at_ms?, decimals?, counter_locale?, grouping?, prefix?, suffix?: liga o contador com os padrões (pt-BR, 0 casas, separador de milhar) e muda só o que veio; os dígitos têm a mesma largura e o número não treme), remove_counter(clip_id: tira o contador e as chaves dele); animate com property counter anima o número. Texto em curva: set_text_path(clip_id de um texto, path_shape?, bend?, path? no custom, path_offset?, path_align?, upright?, path_reverse?, at_ms?), remove_text_path(clip_id); animate com property pathOffset e chaves de abaixo de 0 a acima de 1 faz o texto correr pela curva; letras além das pontas de uma curva aberta somem e curvas fechadas dão a volta. Formato de um contador (valores ilustrativos): [{\"op\":\"set_counter\",\"clip_id\":\"@numero\",\"counter_from\":0,\"counter_to\":2750,\"count_ms\":1100,\"prefix\":\"R$ \"}]. " +
		studioTextRunGuide +
		"animate_text(clip_id ou clip_ids de textos, side, text_style, unit?, duration_ms? de cada letra, palavra ou linha, de 0 a 3000, stagger_ms?, easing?: o padrão do estilo vale quando omitidos), " +
		studioTextAnimatorGuide},
	{name: "faixas-e-projeto", ops: []string{"add_track", "move_track", "update_track", "set_ducking", "clear_ducking", "bake_ducking", "normalize_loudness", "remove_track", "link_clips", "unlink_clips", "set_canvas", "seek", "select"}, guide: "add_track(kind, to_index?, name?), move_track(track_id, to_index), update_track(track_id, name?, hidden?, locked?, muted?, gain_db?, pan?, label_color?, shy?), set_ducking(track_id da faixa que abaixa, trigger_track_ids?, reduce_db?, attack_ms?, release_ms?, hold_ms?, sensitivity?: a faixa abaixa sozinha enquanto há fala nas faixas de gatilho), clear_ducking(track_id), bake_ducking(track_id: troca a regra por chaves gainDb editáveis), normalize_loudness(clip_ids ou clip_id, ou track_id, target_lufs: mede o som como ele toca e ajusta o volume de cada clipe ou o gain_db da faixa para chegar no alvo; a resposta traz o que mediu e o ganho aplicado), remove_track(track_id), " +
		"link_clips(clip_ids: dois ou mais clipes passam a se mover juntos), unlink_clips(clip_ids: separa o vídeo do seu áudio para editar cada um), set_canvas(aspect?, background?, shutter_angle?, shutter_phase?, perspective?), seek(at_ms), select(clip_ids). "},
	{name: "transicoes", groups: studioTransitionGroups, ops: []string{"add_transition", "update_transition", "remove_transition"}, guide: "Transições ficam no corte entre dois clipes que se encostam na mesma faixa, sem mover nenhum; numa faixa de áudio são cruzamentos de som (constantPower ou constantGain): add_transition(clip_id do clipe que sai, to_clip_id?, transition_kind, duration_ms?, alignment?, easing? e as opções do tipo), update_transition(transition_id, os mesmos campos), remove_transition(transition_id). "},
	{name: "layout", ops: []string{"align_clips", "distribute_clips", "sequence_clips", "stagger_clips"}, guide: studioFrameGuide},
	{name: "pais", ops: []string{"parent_clip", "add_null"}, guide: studioParentGuide},
	{name: "comportamentos", ops: []string{"add_behavior", "update_behavior", "remove_behavior", "bake_behavior"}, guide: studioBehaviorGuide},
	{name: "estilo-de-camada", ops: []string{"animate_effect"}, guide: studioClipStyleGuide},
	{name: "rastreamento", ops: []string{"track_point", "attach_to_track", "stabilize_clip", "remove_tracker"}, guide: studioTrackGuide},
	{name: "organizacao", ops: []string{"add_marker", "update_marker", "remove_marker", "mark_beats"}, guide: studioOrganizeGuide},
	{name: "predefinicoes", ops: []string{"apply_animation_preset"}, guide: studioPresetGuide},
	{name: "grupos", ops: []string{"group_clips", "ungroup_clips"}, guide: studioGroupGuide},
	{name: "caminhos", ops: []string{"set_path_ops", "animate_path_op"}, guide: studioPathOpsVideoGuide},
	{name: "repeticao", ops: []string{"set_repeat", "clear_repeat", "animate_repeat"}, guide: studioClipRepeatGuide},
	{name: "elementos", ops: []string{"add_element"}, guide: studioElementGuide},
	{name: "graficos", ops: []string{"add_chart", "update_chart_data", "animate_chart"}, guide: studioChartVideoGuide},
	{name: "tabelas", ops: []string{"add_table", "update_table"}, guide: studioTableVideoGuide},
	{name: "qr", groups: studioQRGroups, ops: []string{"add_qr_code", "update_qr_code"}, guide: studioQRVideoGuide},
}

var studioImageFamilies = []studioOpFamily{
	{name: "criar", groups: studioShapeGroups, ops: []string{"add_text", "add_shape", "add_image", "add_icon", "import_svg"}, guide: "add_text(text, style?, x?, y?, w?, h?, fill?, gradient?, stroke?, font_id?, font_size?, font_weight?, italic?, align?, line_height?, letter_spacing?, highlight?, curve?, shadow?), " +
		"add_shape(shape ou path_preset, path? quando shape é path, points? e inner? na estrela, x, y, w, h, fill? ou gradient?, stroke?, stroke_width?, dash?, radius?, shadow?), add_image(media_id, x?, y?, w?, h?, radius?, frame?, filters?), add_icon(icon_id, x?, y?, w?, h?, fill?), " +
		"import_svg(svg, name?, x?, y?, w?, h?: transforma um desenho em svg em camadas de vetor dentro dessa caixa; texto, imagem e filtros do svg ficam de fora). " +
		"Formato de um degradê radial (posição e cores só ilustram os campos): {\"op\":\"add_shape\",\"shape\":\"ellipse\",\"x\":0.66,\"y\":0.38,\"w\":0.28,\"h\":0.28,\"gradient\":{\"kind\":\"radial\",\"from\":\"#d8efe9\",\"via\":\"#4a9a8c\",\"to\":\"#16332e\"}}. "},
	{name: "camadas", groups: studioShapeGroups, ops: []string{"update_layer", "delete_layers", "duplicate_layers", "order_layers", "align_layers", "distribute_layers", "group_layers", "ungroup_layers", "scaffold_layers", "set_hidden", "set_locked", "replace_image", "fill_frame", "select"}, guide: "update_layer(layer_id ou layer_ids, qualquer campo de posição, texto, estilo ou efeito: gradient, shadow, highlight, curve, blend_mode, frame, clip, flip_x, flip_y, filters, filter_preset, crop, pattern, gradient_map, fills, strokes, effects, clear, name), " +
		"delete_layers(layer_ids), duplicate_layers(layer_ids), order_layers(layer_ids, direction ou above_id ou below_id), align_layers(layer_ids, alignment), distribute_layers(layer_ids, axis), " +
		"group_layers(layer_ids), ungroup_layers(layer_ids), scaffold_layers(layer_ids: a camada de baixo vira o elemento pai das outras), " +
		"set_hidden(layer_ids, hidden), set_locked(layer_ids, locked), replace_image(layer_id, media_id), fill_frame(layer_id de uma moldura ou da foto já dentro dela, media_id: põe a foto recortada na forma, cobrindo-a pelo centro; numa moldura cheia troca a foto sem mover; crop depois desliza a foto dentro da moldura sem tirá-la do lugar), select(layer_ids ou artboard_id). " +
		"Máscara: coloque a forma logo abaixo da imagem (order_layers com below_id) e ligue clip na imagem. " +
		"Tintas e efeitos em camadas (também nas criações): pattern repete um desenho pronto ou uma imagem da biblioteca sobre o fill; gradient_map recolore os tons de uma imagem (duotom); " +
		"fills empilha várias tintas (cor, degradê ou padrão, cada uma com opacity e blend_mode), strokes empilha contornos com width e align próprios, effects junta drop-shadow, inner-shadow, outer-glow e inner-glow. " +
		"As listas substituem os campos simples (fill, gradient, pattern, stroke, stroke_width, shadow) e vão inteiras a cada update; para mudar um efeito, mande o id dele com os campos novos. " +
		"Formato das listas (valores ilustrativos): {\"op\":\"update_layer\",\"layer_id\":\"ID\",\"fills\":[{\"color\":\"#2b3a67\"}],\"effects\":[{\"type\":\"drop-shadow\",\"color\":\"#00000055\",\"y\":6,\"blur\":12},{\"type\":\"inner-glow\",\"color\":\"#c9d4ff\",\"blur\":4,\"opacity\":0.5}]}. "},
	{name: "cor", ops: []string{"set_color", "set_chroma_key", "remove_chroma_key"}, guide: "set_color(layer_id ou layer_ids de imagens, color_grade? com exposure, contrast, highlights, shadows, whites, blacks, temperature, tint, saturation, vibrance, look?, look_intensity?, curves?, curves_preset?, hsl?, lut_media_id?, lut_intensity?, gradient_map?, clear_color?, reset_color?: correção de cor profissional, aplicada depois de filters), " +
		studioColorGuide +
		"set_chroma_key(layer_id ou layer_ids de imagens, chroma_preset? green, blue ou auto, ou chroma_color?, similarity?, smoothness?, spill?: recorta o fundo verde ou azul da foto; auto detecta a cor na borda da imagem; o recorte vem antes dos filtros e da correção de cor), remove_chroma_key(layer_id ou layer_ids). "},
	{name: "formas", ops: []string{"combine_shapes", "set_path_ops"}, guide: "combine_shapes(layer_ids, mode: troca formas e caminhos por um caminho novo, como as operações booleanas dos editores de vetor; a ordem de baixo para cima decide o subtract). " +
		studioPathOpsImageGuide},
	{name: "texto", ops: []string{"style_text"}, guide: studioTextRunGuide},
	{name: "repeticao", ops: []string{"repeat_layers", "set_repeat", "clear_repeat", "expand_repeat"}, guide: "Repetição viva, em vez de duplicar: repeat_layers(layer_ids, repeat: agrupa as camadas e repete o grupo; a resposta traz o id do grupo em created), set_repeat(group_id, repeat), clear_repeat(group_id: tira a repetição e deixa só a original), " +
		"expand_repeat(group_id: transforma as cópias em camadas de verdade para editar uma a uma). As cópias seguem a original na hora: mude a original e todas mudam; studio_read mostra só a original e o repeat do grupo. " +
		"Formato de uma repetição radial (valores ilustrativos): [{\"op\":\"add_shape\",\"ref\":\"petala\",\"shape\":\"ellipse\",\"x\":0.34,\"y\":0.3,\"w\":0.05,\"h\":0.14},{\"op\":\"repeat_layers\",\"layer_ids\":[\"@petala\"],\"repeat\":{\"mode\":\"radial\",\"count\":10,\"radius\":110}}]. "},
	{name: "componentes", ops: []string{"create_component", "add_instance", "set_overrides", "reset_overrides", "swap_component", "detach_instance"}, guide: "Componentes, para um bloco que se repete em várias pranchetas (botão, selo, rodapé): create_component(layer_ids, name?: as camadas viram uma prancheta componente e no lugar fica uma instância; created traz [instância, componente]), " +
		"add_instance(component_id, artboard_id?, x?, y?, w? ou h?: a altura segue a proporção), set_overrides(layer_id da instância, part_id, text?, fill?, stroke?, font_id?, font_weight?, italic?, media_id?, crop?, icon_id?, hidden?: muda só essa instância), " +
		"reset_overrides(layer_id, part_id?, reset?), swap_component(layer_id, component_id), detach_instance(layer_id: vira camadas comuns num grupo), update_artboard(artboard_id, component: true ou false). " +
		"Mudar uma camada do componente muda todas as instâncias na hora; posição, tamanho e ordem das partes vêm do componente. Uma instância não fica dentro de um componente. " +
		"studio_read mostra component e instances nas pranchetas e, em cada instância, component, overrides e parts com o texto e as cores que aparecem. "},
	{name: "estilos", ops: []string{"create_style", "update_style", "apply_style", "detach_style", "delete_style"}, guide: "Estilos compartilhados guardam as cores, os textos e as sombras da marca para o projeto inteiro: create_style(style_kind, name, role?, description?, layer_id e slot? para copiar de uma camada e já ligar, ou artboard_id com slot background, ou style_values com os valores), " +
		"apply_style(style_id, layer_ids ou artboard_id, slot?), update_style(style_id, name?, role?, style_values?, style_clear?: muda todas as camadas ligadas de uma vez), detach_style(layer_ids ou artboard_id, slot?), delete_style(style_id: as camadas ficam com os valores). " +
		"font_size_px vale em pixels de cada prancheta, então o mesmo estilo de título fica do mesmo tamanho no feed e no story; um estilo de texto pode controlar só a fonte. Escrever direto um valor ligado (update_layer fill, font_size...) desliga aquele estilo da camada. studio_read mostra styles com uses e, em cada camada, styles com o estilo ligado em cada campo. "},
	{name: "layout", ops: []string{"set_layout", "set_layout_item", "remove_layout"}, guide: "Layout automático, para botões, cartões e listas que se ajustam sozinhos: set_layout(group_id de um grupo com base, ou layer_ids: a camada de baixo vira a base, ou uma base transparente é criada; layout_direction, layout_gap ou layout_auto_gap, layout_padding, layout_align_main, layout_align_cross, layout_width e layout_height hug ou fixed; o que faltar vem do arranjo atual), " +
		"set_layout_item(layer_id ou group_id de um item, item_width e item_height fixed, fill ou hug (hug só em texto), item_absolute), remove_layout(group_id: as camadas ficam onde estão). " +
		"As posições dos itens vêm do layout: mover um item com update_layer muda a ordem dele no fluxo, mudar o tamanho de um item fill ou hug passa aquele lado para fixed, e a base não gira. Um botão com layout_width hug e o texto com item_width hug cresce quando o texto muda. studio_read mostra layout nos grupos e item nas camadas. "},
	{name: "elementos", ops: []string{"add_element"}, guide: studioElementGuide},
	{name: "graficos", ops: []string{"add_chart", "update_chart_data", "expand_chart"}, guide: studioChartImageGuide},
	{name: "tabelas", ops: []string{"add_table", "update_table"}, guide: studioTableImageGuide},
	{name: "qr", groups: studioQRGroups, ops: []string{"add_qr_code", "update_qr_code"}, guide: studioQRImageGuide},
	{name: "pixels", ops: []string{"add_paint_layer", "paint_area", "crop_to_area", "copy_area_to_layer"}, guide: "Pixels: add_paint_layer(artboard_id?, name?, x?, y?, w?, h?: uma camada de pintura do tamanho da prancheta), paint_area(layer_id, action, area, paint_color?, paint_opacity?: fill e erase em camadas de pintura, hide e reveal em fotos sem perder pixels), " +
		"crop_to_area(layer_id, area: recorta a foto ou a pintura na área), copy_area_to_layer(layer_id, area: copia só a área para uma camada nova, devolvida em created); update_layer com clear mask tira a máscara da foto. "},
	{name: "efeitos", ops: []string{"add_vfx", "update_vfx", "move_vfx", "remove_vfx"}, guide: "Efeitos visuais parados (procedurais, com seed, iguais na tela e em toda exportação): add_vfx(layer_id de uma foto ou, sem layer_id, artboard_id ou a prancheta ativa, vfx_kind, vfx_params?, vfx_seed?, to_index?: numa foto vale só para ela; na prancheta é um ajuste sobre tudo abaixo, o fundo incluído), " +
		"update_vfx(layer_id ou artboard_id, vfx_id?, vfx_params?, vfx_seed?, vfx_hidden?), move_vfx(layer_id ou artboard_id, vfx_id?, to_index), remove_vfx(layer_id ou artboard_id, vfx_id?); os efeitos se aplicam em ordem, do primeiro ao último da lista, e studio_read mostra vfx nas fotos e nas pranchetas. "},
	{name: "pranchetas", ops: []string{"add_artboard", "duplicate_artboard", "update_artboard", "delete_artboard", "move_to_artboard", "arrange_artboards"}, guide: "Pranchetas: o projeto tem uma ou mais, lado a lado (studio_read lista, com x e y de cada uma em pixels; active_artboard é a que o usuário está vendo). x, y, w e h das camadas são frações da prancheta onde elas estão. " +
		"As operações de camada acham a prancheta pelo id da camada; as de criação usam artboard_id ou a prancheta ativa. update_layer, delete_layers, duplicate_layers, order_layers com direction, set_hidden, set_locked, set_color, set_chroma_key, remove_chroma_key e set_path_ops valem para camadas de várias pranchetas de uma vez, a mesma mudança em cada uma, e a resposta traz per_artboard; as outras dependem de uma prancheta só (alinhar, distribuir, agrupar, combinar, above_id e below_id): mande uma por prancheta ou junte as camadas antes com move_to_artboard. " +
		"add_artboard(width, height, name?, background? ou gradient?), duplicate_artboard(artboard_id?, name?, width? e height?: a cópia em outro formato reorganiza as camadas em vez de esticar), " +
		"update_artboard(artboard_id?, name?, width? e height?, background? ou gradient?, component?, artboard_x? e artboard_y?: move a prancheta), delete_artboard(artboard_id: apagar um componente desanexa as instâncias dele), move_to_artboard(layer_ids, artboard_id), " +
		"arrange_artboards(arrange_layout, artboard_ids? (sem elas, todas), arrange_gap?, arrange_order?, arrange_align?, arrange_columns? no grid, arrange_by_size?: organiza as pranchetas a partir do canto onde elas estão, como o arrumar dos editores de design). " +
		"A prancheta criada ou duplicada fica selecionada e passa a ser a ativa; a resposta traz copies com o id de cada camada copiada pelo id original. " +
		"Formato de uma versão (cores ilustrativas): [{\"op\":\"duplicate_artboard\",\"ref\":\"v2\",\"name\":\"Versão 2\"},{\"op\":\"update_artboard\",\"artboard_id\":\"@v2\",\"background\":\"#1e2a24\"},{\"op\":\"update_layer\",\"layer_id\":\"@v2/ID_DO_TITULO\",\"fill\":\"#f2ede3\"}]. "},
}

var (
	studioVideoEditGuide = studioReferenceGuide(studioVideoEditIntro, studioVideoEditOutro, studioVideoFamilies)
	studioImageEditGuide = studioReferenceGuide(studioImageEditIntro, studioImageEditOutro, studioImageFamilies)
	guideWord            = regexp.MustCompile(`[a-z][a-z0-9_]*`)
)

func studioFamiliesOf(kind copilot.StudioKind) []studioOpFamily {
	if kind == copilot.StudioImage {
		return studioImageFamilies
	}
	return studioVideoFamilies
}

func studioReferenceGuide(intro, outro string, families []studioOpFamily) string {
	var b strings.Builder
	b.WriteString(intro)
	for _, family := range families {
		b.WriteString(family.guide)
	}
	b.WriteString(outro)
	return b.String()
}

func studioFamilyIndex(families []studioOpFamily) string {
	lines := make([]string, 0, len(families))
	for _, family := range families {
		lines = append(lines, family.name+": "+strings.Join(family.ops, ", "))
	}
	return strings.Join(lines, "; ")
}

func (f studioOpFamily) words() map[string]bool {
	words := map[string]bool{}
	for _, word := range guideWord.FindAllString(f.guide, -1) {
		words[word] = true
	}
	return words
}

func (f studioOpFamily) fields(order []string) []string {
	words := f.words()
	for _, group := range f.groups {
		for _, field := range reflect.VisibleFields(reflect.TypeOf(group)) {
			if name := copilottools.JSONName(field); name != "" {
				words[name] = true
			}
		}
	}
	var out []string
	for _, name := range order {
		if words[name] {
			out = append(out, name)
		}
	}
	return out
}
