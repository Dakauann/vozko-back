package studiotools

type studioParentArgs struct {
	ParentID     string `json:"parent_id,omitempty" desc:"parent_clip: o clipe que conduz, id de studio_read ou @ref; qualquer clipe visual ou nulo, em qualquer faixa; não vai junto com unparent"`
	Unparent     *bool  `json:"unparent,omitempty" desc:"parent_clip: true solta os clipes do pai mantendo a imagem onde está; não vai junto com parent_id"`
	KeepPosition *bool  `json:"keep_position,omitempty" desc:"parent_clip: true (padrão) mantém a imagem onde está em at_ms e converte os valores para o espaço do novo pai; false deixa os números como estão e o clipe pula, porque eles passam a ser lidos no espaço do pai"`
}

const studioParentGuide = "Vínculo de pai entre clipes visuais, em qualquer faixa: parent_clip(clip_id ou clip_ids, parent_id ou unparent, keep_position?, at_ms? padrão o cursor de tempo: o filho segue a posição, a rotação e a escala do pai em volta da âncora do pai; a opacidade não passa para o filho; fora do tempo do pai, o pai fica parado no valor da ponta mais próxima); " +
	"keep_position true (padrão) mantém a imagem do filho onde está em at_ms, false faz o clipe pular; unparent true solta mantendo a imagem; um vínculo em círculo é recusado. " +
	"Enquanto vinculado, x, y, w, h e rotation do filho são lidos no espaço do pai, de -1 a 2 em x e y (o campo parent do clipe em studio_read diz quem é o pai); apagar um pai passa os filhos para o pai de cima, mantendo a imagem. " +
	"add_null(at_ms? padrão o cursor de tempo, duration_ms? padrão até o fim do vídeo ou 3000, x? e y? de 0 a 1 no centro do nulo, track_id?: cria um nulo, clipe invisível que só conduz filhos, e devolve o id em created): use para animar um grupo como uma peça só (um logo e o texto dele) ou para um movimento de câmera. " +
	"O nulo anda, gira e cresce como qualquer clipe (animate com x, y, position, rotation, scale, scaleX e scaleY); a caixa dele é a escala, então redimensionar o nulo escala os filhos; studio_read mostra os nulos com type null e a caixa. "
