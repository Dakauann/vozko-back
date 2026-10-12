package studiotools

import (
	"fmt"
	"strings"

	"vozko/domain/studio"
)

const (
	qrLinkShort    = "short_link"
	qrLinkWhatsapp = "whatsapp"
	qrLinkURL      = "url"
	qrLinkText     = "text"
)

type studioQRArgs struct {
	QRLink       string   `json:"qr_link,omitempty" enum:"short_link,whatsapp,url,text" desc:"add_qr_code e update_qr_code: o que o código abre; short_link (padrão quando vem qr_url) cria um link curto do workspace para qr_url e conta cada leitura em Links; url abre qr_url direto, sem contar; whatsapp abre uma conversa com qr_phone e a mensagem qr_message; text mostra qr_text"`
	QRURL        string   `json:"qr_url,omitempty" desc:"add_qr_code e update_qr_code: o endereço https:// de destino (short_link ou url)"`
	ShortLinkID  string   `json:"short_link_id,omitempty" desc:"add_qr_code e update_qr_code com short_link: reaproveita um link curto que já existe neste workspace, no lugar de criar outro com qr_url"`
	QRPhone      string   `json:"qr_phone,omitempty" desc:"add_qr_code e update_qr_code com whatsapp: o número com código do país e DDD, só dígitos, como 5511999998888"`
	QRMessage    string   `json:"qr_message,omitempty" desc:"add_qr_code e update_qr_code com whatsapp: a mensagem que já vem escrita na conversa, opcional"`
	QRText       string   `json:"qr_text,omitempty" desc:"add_qr_code e update_qr_code com text: o texto guardado no código"`
	QREcc        string   `json:"qr_ecc,omitempty" enum:"L,M,Q,H" desc:"correção de erro: L guarda mais conteúdo, H aguenta mais sujeira e o maior logo; M é o padrão e H entra sozinho quando há logo"`
	QRQuietZone  *int     `json:"qr_quiet_zone,omitempty" desc:"margem clara em volta do código, em módulos, de 2 a 16 (4 padrão)"`
	QRDark       string   `json:"qr_dark,omitempty" desc:"cor dos módulos, #rrggbb; precisa ser mais escura que qr_light, com contraste de pelo menos 4"`
	QRLight      string   `json:"qr_light,omitempty" desc:"cor do fundo do código, #rrggbb"`
	LogoMediaID  string   `json:"logo_media_id,omitempty" desc:"add_qr_code e update_qr_code: imagem da biblioteca no centro do código"`
	LogoScale    *float64 `json:"logo_scale,omitempty" desc:"lado do logo como fração do lado do código: até 0.2 com M, 0.25 com Q e 0.3 com H; o código cresce sozinho para continuar lendo"`
	QRRemoveLogo *bool    `json:"qr_remove_logo,omitempty" desc:"update_qr_code: true tira o logo"`
}

func (a studioQRArgs) isShortLink() bool {
	return a.QRLink == qrLinkShort || (a.QRLink == "" && a.ShortLinkID != "")
}

func (a studioQRArgs) argsIssue() string {
	for _, color := range []string{a.QRDark, a.QRLight} {
		if color != "" && (len(color) != 7 || !chartColor(color)) {
			return "qr_dark e qr_light são cores #rrggbb, sem transparência"
		}
	}
	switch {
	case a.QRQuietZone != nil && (*a.QRQuietZone < studio.MinQRQuietZone || *a.QRQuietZone > studio.MaxQRQuietZone):
		return fmt.Sprintf("qr_quiet_zone vai de %d a %d módulos", studio.MinQRQuietZone, studio.MaxQRQuietZone)
	case a.LogoScale != nil && (*a.LogoScale < studio.MinQRLogoScale || *a.LogoScale > studio.QRLogoMax("H")):
		return fmt.Sprintf("logo_scale vai de %g a %g do lado do código", studio.MinQRLogoScale, studio.QRLogoMax("H"))
	case a.QREcc != "" && a.LogoScale != nil && *a.LogoScale > studio.QRLogoMax(a.QREcc):
		return fmt.Sprintf("com qr_ecc %s o logo vai até %g do lado do código", a.QREcc, studio.QRLogoMax(a.QREcc))
	case a.QRLink == qrLinkShort && a.QRURL == "" && a.ShortLinkID == "":
		return "short_link precisa de qr_url com o destino ou de short_link_id de um link deste workspace"
	case a.QRLink == qrLinkWhatsapp && strings.TrimSpace(a.QRPhone) == "":
		return "whatsapp precisa de qr_phone"
	case a.QRLink == qrLinkURL && a.QRURL == "":
		return "url precisa de qr_url"
	case a.QRLink == qrLinkText && strings.TrimSpace(a.QRText) == "":
		return "text precisa de qr_text"
	case a.ShortLinkID != "" && a.QRURL != "":
		return "mande qr_url para criar um link curto ou short_link_id para reaproveitar um, não os dois"
	}
	return ""
}

const studioQRIntro = "QR Code gerado no editor, nítido em qualquer tamanho e igual na tela e na exportação. Por padrão aponte para um link curto do workspace (qr_url com o destino): cada leitura conta em Links, com país e aparelho; use qr_link url só quando o usuário pedir o endereço direto. " +
	"A leitura é conferida: cores sem contraste, código invertido, margem pequena demais ou logo grande demais para o nível são recusados com o motivo. " +
	"update_qr_code troca só o que vier; studio_read mostra o QR em qr, com link, content, ecc, cores, logo e a versão. " +
	"Exemplos: {\"op\":\"add_qr_code\",\"qr_url\":\"https://loja.com/oferta\"}; {\"op\":\"add_qr_code\",\"qr_link\":\"whatsapp\",\"qr_phone\":\"5511999998888\",\"qr_message\":\"Quero saber mais\",\"logo_media_id\":\"ID\"}. "

const studioQRImageGuide = studioQRIntro +
	"add_qr_code(qr_link?, qr_url? ou short_link_id? ou qr_phone e qr_message? ou qr_text?, qr_ecc?, qr_quiet_zone?, qr_dark?, qr_light?, logo_media_id?, logo_scale?, name?, artboard_id?, x?, y?, w?: o código é quadrado), update_qr_code(layer_id, os campos que mudam, qr_remove_logo?). "

const studioQRVideoGuide = studioQRIntro +
	"add_qr_code(os mesmos campos, at_ms, duration_ms?, track_id?, x?, y?, w?), update_qr_code(clip_id, os campos que mudam, qr_remove_logo?). "
