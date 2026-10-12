package studiotools

import (
	"fmt"

	"vozko/domain/studio"
)

type studioTiltArgs struct {
	RotationX   *float64 `json:"rotation_x,omitempty" desc:"update_clip: giro fixo em profundidade em volta do eixo horizontal, em graus de -3600 a 3600, pela âncora do clipe (vídeo, imagem, texto, forma, ícone e grupo); positivo leva o topo para trás; 0 tira; para animar use animate com property rotationX"`
	RotationY   *float64 `json:"rotation_y,omitempty" desc:"update_clip: giro fixo em profundidade em volta do eixo vertical, em graus de -3600 a 3600; positivo leva o lado direito para trás, 180 mostra o verso; 0 tira; para animar use animate com property rotationY"`
	Perspective *float64 `json:"perspective,omitempty" desc:"set_canvas: ângulo de visão do projeto em graus, de 0 a 120, que dá a perspectiva das rotações 3D; 40 é o padrão, maior deixa a perspectiva mais forte e 0 deixa plano"`
}

func (a studioTiltArgs) argsIssue() string {
	for _, field := range []struct {
		name  string
		value *float64
	}{{"rotation_x", a.RotationX}, {"rotation_y", a.RotationY}} {
		if field.value != nil && (*field.value < -studio.MaxTilt || *field.value > studio.MaxTilt) {
			return fmt.Sprintf("%s vai de %g a %g graus", field.name, -studio.MaxTilt, studio.MaxTilt)
		}
	}
	if a.Perspective != nil && (*a.Perspective < studio.MinPerspective || *a.Perspective > studio.MaxPerspective) {
		return fmt.Sprintf("perspective vai de %g a %g graus", studio.MinPerspective, studio.MaxPerspective)
	}
	return ""
}

const studioTiltGuide = "Rotação 3D: clipes de vídeo, imagem, texto, forma, ícone e grupo giram em profundidade pela âncora, como uma carta virando. update_clip(clip_id ou clip_ids, rotation_x?, rotation_y?) dá um giro fixo; animate com property rotationX ou rotationY e chaves em graus anima o giro (rotationY de 0 a 180 vira o clipe e mostra o verso espelhado; 90 deixa o clipe de perfil, invisível). " +
	"A ordem é fixa: primeiro rotation (no plano), depois rotationY, depois rotationX. set_canvas(perspective?) muda o ângulo de visão do projeto, de 0 a 120 graus (40 é o padrão; 0 deixa plano, sem o lado de perto crescer); o ponto de fuga é o centro do quadro. " +
	"Letras virando: nos animadores de texto, animator_properties com rotationX e rotationY em graus gira cada letra, palavra ou linha em profundidade em volta do seu centro (o preset flipIn faz as letras entrarem virando). O giro de um pai não passa para os filhos. studio_read mostra rotation_x e rotation_y no clipe e perspective no projeto quando usados. "
