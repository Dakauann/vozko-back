package studiotools

type studioPointArgs struct {
	X float64 `json:"x" req:"true" desc:"posição horizontal no quadro do vídeo, de 0 (esquerda) a 1 (direita), como aparece em studio_look"`
	Y float64 `json:"y" req:"true" desc:"posição vertical no quadro do vídeo, de 0 (topo) a 1 (base)"`
}

type studioTrackArgs struct {
	Point             *studioPointArgs `json:"point,omitempty" desc:"track_point: o ponto a seguir, no quadro do vídeo no instante at_ms; escolha algo com contraste (um olho, uma quina, uma letra), nunca uma área lisa"`
	TrackSize         *float64         `json:"track_size,omitempty" desc:"track_point: lado do quadrado de detalhe em volta do ponto, em fração da altura do quadro, de 0.01 a 0.5 (padrão 0.06); maior segura melhor um objeto grande, menor segue um detalhe pequeno"`
	FromMS            *int64           `json:"from_ms,omitempty" desc:"track_point: começo do trecho rastreado na linha do tempo, dentro do clipe (padrão o início do clipe)"`
	ToMS              *int64           `json:"to_ms,omitempty" desc:"track_point: fim do trecho rastreado na linha do tempo, dentro do clipe (padrão o fim do clipe)"`
	TrackDirection    string           `json:"track_direction,omitempty" enum:"both,forward,backward" desc:"track_point: para que lado rastrear a partir de at_ms; both (padrão) vai para frente e para trás"`
	TrackerID         string           `json:"tracker_id,omitempty" desc:"o ponto rastreado: id do campo trackers do clipe em studio_read ou @ref de track_point; em track_point, refaz esse ponto mantendo o id e quem o segue"`
	SecondTrackerID   string           `json:"second_tracker_id,omitempty" desc:"stabilize_clip: segundo ponto do mesmo clipe, para estabilizar também a rotação e a escala"`
	KeepPartial       *bool            `json:"keep_partial,omitempty" desc:"track_point: true guarda o trecho rastreado até onde o ponto foi perdido; sem ele, perder o ponto recusa a operação"`
	AttachMode        string           `json:"attach_mode,omitempty" enum:"link,keys" desc:"attach_to_track: link (padrão) faz o clipe seguir o ponto ao vivo, como um vínculo de pai; keys grava quadros-chave de posição em cada quadro"`
	StabilizeRotation *bool            `json:"stabilize_rotation,omitempty" desc:"stabilize_clip: true também desfaz o giro da câmera; pede second_tracker_id"`
	StabilizeScale    *bool            `json:"stabilize_scale,omitempty" desc:"stabilize_clip: true também desfaz a aproximação e o afastamento; pede second_tracker_id"`
	SmoothMS          *int64           `json:"smooth_ms,omitempty" desc:"stabilize_clip: de 100 a 10000 ms; suaviza o movimento em vez de travar (tira o tremor e mantém a panorâmica); sem ele, o ponto fica parado onde estava no quadro de referência"`
	Zoom              *float64         `json:"zoom,omitempty" desc:"stabilize_clip: zoom de 1 a 4 para esconder as bordas; sem ele, o menor zoom que esconde as bordas em todo o clipe é calculado"`
	Unstabilize       *bool            `json:"unstabilize,omitempty" desc:"stabilize_clip: true tira a estabilização do clipe"`
}

const studioTrackGuide = "Rastreamento de pontos em clipes de vídeo, feito no navegador sobre os quadros do vídeo: track_point(clip_id, point {x, y} no quadro em at_ms (padrão o cursor de tempo), track_size?, from_ms? e to_ms? para um trecho, track_direction?, name? para o nome do ponto, tracker_id? para refazer um ponto, keep_partial?) cria um ponto e devolve o id em created e a confiança média e mínima nas notas; o ponto é perdido quando a confiança cai abaixo de 0.6 e então a operação é recusada dizendo onde, salvo com keep_partial true. " +
	"Rastreie num lote próprio e só clipes que já existem; o rastreio de um lote tem até 20 s, então divida clipes longos com from_ms e to_ms. " +
	"attach_to_track(clip_id ou clip_ids, parent_id o vídeo rastreado, tracker_id, attach_mode link ou keys, keep_position? padrão true, at_ms?) faz um texto, adesivo, imagem ou nulo seguir o ponto: link segue ao vivo como um vínculo de pai em volta do ponto (parent e parent_tracker em studio_read), keys grava quadros-chave de x e y em cada quadro; keep_position false põe o clipe em cima do ponto. Para soltar, parent_clip com unparent. " +
	"stabilize_clip(clip_id, tracker_id, second_tracker_id?, stabilize_rotation?, stabilize_scale?, smooth_ms?, zoom?) segura o tremor da câmera: sem smooth_ms o ponto fica parado; com smooth_ms o movimento fica suave; o zoom esconde as bordas (o campo stabilize de studio_read mostra o zoom); unstabilize true tira. remove_tracker(clip_id, tracker_id) apaga um ponto, solta quem o seguia mantendo a imagem e tira a estabilização feita com ele. " +
	"Confira com studio_look depois de rastrear e de estabilizar. "
