package shared

import (
	"fmt"
	"strings"
	"time"
)

type ChannelType string

const (
	ChannelWhatsApp  ChannelType = "whatsapp"
	ChannelMessaging ChannelType = "messaging"
)

type ConversationContext struct {
	Channel         ChannelType
	UserPhoneNumber string
	UserName        string
	ConversationID  string
	StartTime       time.Time
	CampaignName    string
	AgentName       string
	Metadata        map[string]interface{}
	AvailableTools  []string
}

func (c ConversationContext) BuildContextPrompt() string {
	var sb strings.Builder
	sb.WriteString("\n--- Conversation Context ---\n")

	switch c.Channel {
	case ChannelWhatsApp:
		sb.WriteString("CANAL: WHATSAPP (mensagens de texto)\n")
		sb.WriteString("IMPORTANTE: Você está respondendo via WhatsApp. NÃO diga frases como 'enviei uma mensagem para o seu WhatsApp' - você JÁ ESTÁ no WhatsApp. Suas respostas aparecem diretamente para o usuário aqui.\n\n")
	case ChannelMessaging:
		sb.WriteString("CANAL: CHAT DE MENSAGENS (mensagens de texto)\n")
		sb.WriteString("IMPORTANTE: Você está respondendo em um chat de teste por texto. NÃO diga que vai enviar algo para WhatsApp ou para outro canal, a menos que realmente use uma ferramenta para isso.\n\n")
	}

	sb.WriteString("REGRAS DE IDENTIDADE (OBRIGATÓRIO):\n")
	sb.WriteString("- Seu nome e identidade são definidos EXCLUSIVAMENTE pelo seu system prompt ou messaging prompt.\n")
	sb.WriteString("- NUNCA use o nome do lead/contato como seu próprio nome.\n")
	sb.WriteString("- NUNCA diga '[Seu Nome]', '[Nome]' ou qualquer placeholder, use SOMENTE o nome que está no seu system prompt.\n")

	if c.hasTools() {
		sb.WriteString("\nUSO DE FERRAMENTAS (OBRIGATÓRIO):\n")
		sb.WriteString("- Você tem ferramentas (functions) disponíveis. Quando precisar executar uma ação, você DEVE usar a chamada de função (tool call / function call).\n")
		sb.WriteString("- NUNCA apenas diga que vai fazer algo, você DEVE chamar a ferramenta correspondente.\n")
		sb.WriteString("- Se o cliente pedir ou concordar com uma ação que uma ferramenta pode executar, chame a ferramenta IMEDIATAMENTE.\n\n")
	}
	sb.WriteString("- Se seu system prompt não define um nome, NÃO invente um. Apenas prossiga sem se apresentar novamente.\n\n")

	if c.Channel != ChannelMessaging && c.hasTool("manage_entry_stage") {
		sb.WriteString("CLASSIFICAÇÃO DO LEAD (OBRIGATÓRIO - TODA MENSAGEM):\n")
		sb.WriteString("Em CADA resposta sua, você DEVE chamar a ferramenta 'manage_entry_stage' para classificar o lead.\n")
		sb.WriteString("Chame a ferramenta JUNTO com sua resposta de texto, não espere ser solicitado.\n")
		sb.WriteString("As etapas disponíveis estão pré-carregadas no parâmetro 'target_stage_name' da ferramenta.\n")
		sb.WriteString("Use SOMENTE etapas listadas no enum. NUNCA invente ou adivinhe nomes de etapas.\n")
		sb.WriteString("Se a etapa atual já está correta, chame a ferramenta mesmo assim passando a mesma etapa.\n")
		sb.WriteString("FLUXO OBRIGATÓRIO: 1) Analise a mensagem do cliente → 2) Chame manage_entry_stage → 3) Responda ao cliente.\n\n")
	}

	if c.Channel == ChannelWhatsApp && c.hasTool("send_whatsapp_template") {
		sb.WriteString("ENVIO DE WHATSAPP (OBRIGATÓRIO):\n")
		sb.WriteString("Você tem a ferramenta 'send_whatsapp_template' disponível para enviar mensagens de WhatsApp.\n")
		sb.WriteString("Quando o cliente ACEITAR receber uma mensagem no WhatsApp (ex: 'pode sim', 'pode enviar', 'sim', 'ok', 'tá bom'),\n")
		sb.WriteString("você DEVE OBRIGATORIAMENTE chamar a ferramenta 'send_whatsapp_template' imediatamente.\n")
		sb.WriteString("NUNCA diga 'vou enviar' ou 'enviarei' sem de fato chamar a ferramenta, isso é PROIBIDO.\n")
		sb.WriteString("O número do cliente e o template já estão configurados automaticamente.\n")
		sb.WriteString("Se o template tiver parâmetros (placeholders {{1}}, {{2}}...), preencha-os com os dados do contexto.\n")
		sb.WriteString("FLUXO: 1) Cliente aceita → 2) Chame send_whatsapp_template → 3) Confirme o envio ao cliente.\n\n")
	}

	if c.hasTool("search_knowledge_base") {
		sb.WriteString("BASE DE CONHECIMENTO (OBRIGATÓRIO):\n")
		sb.WriteString("Você tem a ferramenta 'search_knowledge_base' para pesquisar nos documentos da empresa.\n")
		sb.WriteString("Antes de responder perguntas sobre produtos, preços, políticas, prazos ou condições, chame a ferramenta — não responda esses dados de memória.\n")
		sb.WriteString("Baseie a resposta nos trechos retornados. Busque pelos termos-chave do assunto em uma frase específica, não pela mensagem literal do cliente.\n")
		sb.WriteString("Se a busca não trouxer nada útil, reformule com sinônimos ou termos mais amplos/específicos e tente de novo (até 2-3 buscas) antes de desistir.\n")
		sb.WriteString("Só depois de esgotar as buscas responda com base nas suas próprias instruções; se a informação não estiver em lugar nenhum, diga que não a tem em vez de inventar.\n\n")
	}

	sb.WriteString("--- Lead/Contact Info (the person you are talking to) ---\n")
	switch c.Channel {
	case ChannelWhatsApp:
		sb.WriteString(fmt.Sprintf("Lead WhatsApp Number: %s\n", c.UserPhoneNumber))
		if c.UserName != "" {
			sb.WriteString(fmt.Sprintf("Lead Name: %s\n", c.UserName))
		}
		sb.WriteString(fmt.Sprintf("Conversation ID: %s\n", c.ConversationID))
	default:
		sb.WriteString(fmt.Sprintf("Lead Contact Number: %s\n", c.UserPhoneNumber))
		if c.UserName != "" {
			sb.WriteString(fmt.Sprintf("Lead Name: %s\n", c.UserName))
		}
		sb.WriteString(fmt.Sprintf("Conversation ID: %s\n", c.ConversationID))
	}

	if c.CampaignName != "" {
		sb.WriteString(fmt.Sprintf("Campaign: %s\n", c.CampaignName))
	}

	if len(c.Metadata) > 0 {
		sb.WriteString("--- Lead Metadata ---\n")
		for key, value := range c.Metadata {
			sb.WriteString(fmt.Sprintf("%s: %v\n", key, value))
		}
	}

	sb.WriteString("--- End Context ---\n\n")
	return sb.String()
}

func (c ConversationContext) hasTools() bool {
	return len(c.AvailableTools) > 0
}

func (c ConversationContext) hasTool(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	for _, t := range c.AvailableTools {
		if strings.ToLower(strings.TrimSpace(t)) == lower {
			return true
		}
	}
	return false
}
