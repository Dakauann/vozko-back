package businessphone

import (
	"time"

	"vozko/domain/notification"
)

func QualityNotice(restricted bool) PhoneNotice {
	label, severity, state := "Em risco", notification.SeverityWarning, "quality:"+string(StatusFlagged)
	if restricted {
		label, severity, state = "Restrita", notification.SeverityCritical, "quality:"+string(StatusRestricted)
	}
	return PhoneNotice{
		State:    state,
		Severity: severity,
		Title:    "Qualidade do número em risco",
		Body:     "A Meta reduziu a classificação de qualidade do número. Qualidade baixa limita o volume de mensagens e, se persistir, leva à restrição do número.",
		Subject:  "Qualidade do número em risco",
		Template: "whatsapp_quality_alert.html",
		Extra:    map[string]interface{}{"Quality": label},
	}
}

func NameApprovedNotice(verifiedName string) PhoneNotice {
	return accountUpdateNotice("name_approved", notification.SeveritySuccess, "Nome de exibição aprovado", verifiedName,
		"O nome de exibição do seu número de WhatsApp foi aprovado e já está ativo.", "Aprovado", "success", "check")
}

func NameRejectedNotice() PhoneNotice {
	return accountUpdateNotice("name_rejected", notification.SeverityWarning, "Nome de exibição reprovado", "Ação necessária",
		"O nome de exibição solicitado para o seu número de WhatsApp não foi aprovado. Acesse o painel para enviar um novo nome.", "Reprovado", "warning", "alert")
}

func VerifiedNotice() PhoneNotice {
	n := accountUpdateNotice("verified", notification.SeveritySuccess, "Conta verificada", "O seu número foi marcado como empresa oficial",
		"A sua conta WhatsApp foi verificada e o seu número agora aparece como empresa oficial para os seus clientes.", "Verificado", "success", "check")
	n.Subject = "Conta WhatsApp verificada"
	return n
}

func ReinstatedNotice() PhoneNotice {
	n := accountUpdateNotice("reinstated", notification.SeveritySuccess, "Conta restaurada", "Os seus números estão ativos novamente",
		"A sua conta WhatsApp foi restaurada. Os seus números já podem enviar e receber mensagens normalmente.", "Ativo", "success", "check")
	n.Subject = "Conta WhatsApp restaurada"
	return n
}

func accountUpdateNotice(state string, severity notification.Severity, headline, subtitle, message, statusLabel, tone, glyph string) PhoneNotice {
	return PhoneNotice{
		State:    state,
		Severity: severity,
		Title:    headline,
		Body:     message,
		Subject:  headline,
		Template: "whatsapp_account_update.html",
		Extra: map[string]interface{}{
			"Headline": headline, "Subtitle": subtitle, "Message": message,
			"StatusLabel": statusLabel, "Tone": tone, "Glyph": glyph,
		},
	}
}

func BannedNotice() PhoneNotice {
	return PhoneNotice{
		State:    "banned",
		Severity: notification.SeverityCritical,
		Title:    "Número de WhatsApp bloqueado",
		Body:     "A Meta bloqueou o número. Enquanto estiver bloqueado, ele não envia nem recebe mensagens.",
		Subject:  "Número de WhatsApp bloqueado",
		Template: "whatsapp_number_banned.html",
	}
}

func ScheduledDisableNotice(disableDate string) PhoneNotice {
	return PhoneNotice{
		State:    "scheduled_disable",
		Severity: notification.SeverityCritical,
		Title:    "Número agendado para desativação",
		Body:     "A Meta agendou a desativação do número para " + disableDate + ". Aja antes dessa data para não perder o número.",
		Subject:  "Número agendado para desativação",
		Template: "whatsapp_scheduled_disable.html",
		Extra:    map[string]interface{}{"DisableDate": disableDate},
	}
}

func SuspendedNotice() PhoneNotice {
	return PhoneNotice{
		State:    "suspended",
		Severity: notification.SeverityCritical,
		Title:    "Número de WhatsApp suspenso",
		Body:     "O número foi suspenso porque o complemento ou o plano que o cobria deixou de estar ativo. Ele não envia nem recebe mensagens enquanto estiver suspenso.",
		Subject:  "Número de WhatsApp suspenso",
		Template: "whatsapp_number_suspended.html",
		Extra:    map[string]interface{}{"Reason": "Complemento ou plano inativo"},
		Window:   time.Hour,
	}
}

func NumberLiveNotice() PhoneNotice {
	return PhoneNotice{
		State:    "live",
		Severity: notification.SeveritySuccess,
		Title:    "Seu número de WhatsApp está ativo",
		Body:     "A configuração do número foi concluída. Ele já pode enviar e receber mensagens pelos seus agentes e campanhas.",
		Subject:  "Seu número de WhatsApp está ativo",
		Template: "whatsapp_number_live.html",
		Window:   30 * 24 * time.Hour,
	}
}

func OnboardingFailedNotice(reason string) PhoneNotice {
	return PhoneNotice{
		State:    "onboarding_failed",
		Severity: notification.SeverityCritical,
		Title:    "Falha na configuração do número",
		Body:     "Não foi possível concluir a configuração do número. Ele continua visível no painel e você pode tentar novamente.",
		Subject:  "Falha na configuração do número",
		Template: "whatsapp_onboarding_failed.html",
		Extra:    map[string]interface{}{"Reason": reason},
		Window:   24 * time.Hour,
	}
}
