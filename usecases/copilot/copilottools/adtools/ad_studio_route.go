package adtools

import (
	"fmt"

	"vozko/domain/advertising"
	"vozko/domain/copilot"
)

type studioAdRoute struct{}

func (studioAdRoute) OfferedOn(view copilot.View) bool {
	return !view.Focused() || view.OnStudio()
}

const bytesPerMB = 1 << 20

var mediaSpecTexts = map[string]string{
	advertising.CodeMediaFormat: "a Meta aceita imagem em PNG ou JPEG e vídeo em MP4 ou MOV; exporte de novo nesse formato",
	advertising.CodeMediaTooLarge: fmt.Sprintf("o arquivo passa do limite da Meta (imagem até %d MB, vídeo até %d GB); exporte menor",
		advertising.MaxImageBytes/bytesPerMB, advertising.MaxVideoBytes/(bytesPerMB<<10)),
	advertising.CodeMediaTooSmall: fmt.Sprintf("o arquivo é pequeno demais para a Meta (imagem com pelo menos %d px de largura e de altura, vídeo com %d px); exporte em escala maior",
		advertising.MinImageSide, advertising.MinVideoSide),
	advertising.CodeMediaRatio:      "a proporção fica fora do que a Meta veicula (imagem de 9:16 a 1,91:1, vídeo de 9:16 a 16:9); ajuste o tamanho da prancheta ou o formato do vídeo",
	advertising.CodeVideoTooShort:   fmt.Sprintf("o vídeo precisa ter pelo menos %d segundo", advertising.MinVideoMS/1000),
	advertising.CodeVideoTooLong:    fmt.Sprintf("o vídeo passa de %d minutos, o limite da Meta", advertising.MaxVideoMS/60_000),
	advertising.CodeMediaUnreadable: "o arquivo não pôde ser lido; exporte de novo",
}
