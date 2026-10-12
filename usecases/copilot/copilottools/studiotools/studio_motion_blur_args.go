package studiotools

import (
	"fmt"

	"vozko/domain/studio"
)

type studioMotionBlurArgs struct {
	MotionBlur   *bool    `json:"motion_blur,omitempty" desc:"update_clip: true borra o clipe na direção em que ele anda, gira ou cresce, como uma câmera (vídeo, imagem, texto, forma e ícone); false desliga; o rastro vem das chaves, do trajeto, do pai e da entrada deslizando"`
	ShutterAngle *float64 `json:"shutter_angle,omitempty" desc:"set_canvas: ângulo do obturador do projeto em graus, de 0 a 720; 180 (padrão) é o rastro natural de cinema, 360 dobra, 90 deixa mais seco e 0 desliga o desfoque de movimento de todos os clipes"`
	ShutterPhase *float64 `json:"shutter_phase,omitempty" desc:"set_canvas: fase do obturador em graus, de -360 a 360: quando o rastro começa em relação ao quadro; -90 (padrão) centraliza o rastro com ângulo 180, 0 deixa o rastro só depois do quadro e -180 só antes"`
}

func (a studioMotionBlurArgs) argsIssue() string {
	switch {
	case a.ShutterAngle != nil && (*a.ShutterAngle < studio.MinShutterAngle || *a.ShutterAngle > studio.MaxShutterAngle):
		return fmt.Sprintf("shutter_angle vai de %g a %g graus", studio.MinShutterAngle, studio.MaxShutterAngle)
	case a.ShutterPhase != nil && (*a.ShutterPhase < studio.MinShutterPhase || *a.ShutterPhase > studio.MaxShutterPhase):
		return fmt.Sprintf("shutter_phase vai de %g a %g graus", studio.MinShutterPhase, studio.MaxShutterPhase)
	}
	return ""
}

const studioMotionBlurGuide = "Desfoque de movimento: update_clip(clip_id ou clip_ids, motion_blur true ou false) liga o rastro de movimento num clipe de vídeo, imagem, texto, forma ou ícone; ele aparece só quando o clipe se move (chaves de x, y, position, scale, rotation, o trajeto curvo, o pai que se move, a entrada deslizando, o tremor) e some quando o clipe está parado. " +
	"set_canvas(shutter_angle?, shutter_phase?) ajusta o obturador do projeto, que vale para todos os clipes com o desfoque ligado: 180 graus é o padrão, maior deixa o rastro mais longo e 0 desliga; a fase -90 centraliza o rastro no quadro. studio_read mostra motion_blur no clipe e shutter_angle e shutter_phase no projeto. "
