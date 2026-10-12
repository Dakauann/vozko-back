package studiotools

import (
	"fmt"

	"vozko/domain/studio"
)

type studioCameraArgs struct {
	CameraPanX     *float64 `json:"camera_pan_x,omitempty" desc:"set_camera: para onde a câmera do clipe de efeito olha na horizontal, em frações da largura do quadro, de -2 a 2; 0 é o centro, 0.25 mostra o que está à direita"`
	CameraPanY     *float64 `json:"camera_pan_y,omitempty" desc:"set_camera: para onde a câmera olha na vertical, em frações da altura do quadro, de -2 a 2; 0 é o centro, positivo mostra o que está embaixo"`
	CameraZoom     *float64 `json:"camera_zoom,omitempty" desc:"set_camera: aproximação da câmera, de 0.1 a 10; 1 mostra o quadro inteiro, 2 aproxima o dobro, 0.5 afasta"`
	CameraRotation *float64 `json:"camera_rotation,omitempty" desc:"set_camera: quanto tudo o que está abaixo gira, em graus, de -3600 a 3600; positivo é horário"`
	ClearCamera    *bool    `json:"clear_camera,omitempty" desc:"set_camera: true tira a câmera e as chaves dela do clipe de efeito"`
}

func (a studioCameraArgs) argsIssue() string {
	switch {
	case a.CameraPanX != nil && (*a.CameraPanX < -studio.MaxCameraPan || *a.CameraPanX > studio.MaxCameraPan):
		return fmt.Sprintf("camera_pan_x vai de %g a %g", -studio.MaxCameraPan, studio.MaxCameraPan)
	case a.CameraPanY != nil && (*a.CameraPanY < -studio.MaxCameraPan || *a.CameraPanY > studio.MaxCameraPan):
		return fmt.Sprintf("camera_pan_y vai de %g a %g", -studio.MaxCameraPan, studio.MaxCameraPan)
	case a.CameraZoom != nil && (*a.CameraZoom < studio.MinCameraZoom || *a.CameraZoom > studio.MaxCameraZoom):
		return fmt.Sprintf("camera_zoom vai de %g a %g", studio.MinCameraZoom, studio.MaxCameraZoom)
	case a.CameraRotation != nil && (*a.CameraRotation < -studio.MaxCameraRotation || *a.CameraRotation > studio.MaxCameraRotation):
		return fmt.Sprintf("camera_rotation vai de %g a %g graus", -studio.MaxCameraRotation, studio.MaxCameraRotation)
	case a.ClearCamera != nil && *a.ClearCamera && (a.CameraPanX != nil || a.CameraPanY != nil || a.CameraZoom != nil || a.CameraRotation != nil):
		return "clear_camera tira a câmera inteira; mande sem os campos camera_*"
	}
	return ""
}

const studioAdjustmentGuide = "Clipe de ajuste (o clipe de efeito vale para tudo o que está abaixo dele, só durante o tempo dele, igual na prévia e na exportação): add_effect_clip sem vfx_kind cria um clipe de efeito vazio para receber o ajuste. " +
	"Nele, set_color aplica a correção inteira (color_grade, look, curves, hsl, lut_media_id e lut_intensity, gradient_map) a tudo o que está abaixo; update_clip blur desfoca o que está abaixo; add_mask limita o ajuste a uma forma (fora dela tudo fica como estava; feather suaviza a borda). " +
	"set_camera(clip_id do clipe de efeito, camera_pan_x?, camera_pan_y?, camera_zoom?, camera_rotation?, at_ms? para gravar uma chave quando o campo já é animado, clear_camera?) é uma câmera 2D que move, aproxima e gira tudo o que está abaixo; update_clip motion_blur true borra o movimento dessa câmera pelo obturador do projeto. " +
	"animate anima blur, os campos da correção (exposure ... vibrance) e a câmera (cameraPanX, cameraPanY, cameraZoom, cameraRotation) do clipe de efeito; opacity e os fades dosam a cor, o desfoque, a câmera e os efeitos juntos. Ordem dentro do clipe: câmera, correção, LUT, curvas, cor seletiva, mapa de gradiente, desfoque, efeitos, e por fim a máscara. studio_read mostra camera, color_grade, gradient_map, blur, masks, motion_blur e strength no clipe. "
