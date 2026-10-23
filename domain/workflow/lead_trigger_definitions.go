package workflow

import "strconv"

func floatRef(v float64) *float64 { return &v }

func leadTriggerOutputKeys(extra ...OutputKeyDefinition) []OutputKeyDefinition {
	return append([]OutputKeyDefinition{
		{Key: DataKeyLeadID, Description: "ID do lead"},
		{Key: DataKeyLeadName, Description: "Nome do lead"},
	}, extra...)
}

func birthdayTriggerDefinition() NodeDefinition {
	return NodeDefinition{
		Type:          NodeTypeTriggerBirthday,
		Category:      NodeCategoryTrigger,
		TestBehavior:  NodeTestTrigger,
		Scopes:        []NodeScope{NodeScopeWhatsApp},
		Label:         "Aniversário",
		Description:   "Dispara uma vez no dia do aniversário de cada lead, no fuso horário da área de trabalho.",
		Icon:          "CalendarCheck",
		DefaultConfig: map[string]interface{}{BirthdayHourKey: DefaultBirthdayHour},
		ConfigSchema: []ConfigField{
			{Key: BirthdayHourKey, Label: "Hora do disparo", Type: "number", Placeholder: "9", Min: floatRef(0), Max: floatRef(23), Step: floatRef(1),
				Description: "Hora do dia (0 a 23) a partir da qual o fluxo começa para os aniversariantes do dia."},
		},
		OutputKeys: leadTriggerOutputKeys(OutputKeyDefinition{Key: DataKeyBirthday, Description: "Data do aniversário (AAAA-MM-DD)"}),
		Guidance: NodeGuidance{
			When: "Para parabenizar o lead no dia do aniversário.",
			Behavior: "Usa a data de nascimento do cadastro do lead. Começa uma execução por lead e por dia, mesmo sem conversa aberta; 29 de fevereiro conta no dia 28 em anos não bissextos. " +
				"Disponibiliza {{lead_id}}, {{lead_name}} e {{birthday}}. Nós que dependem de uma conversa seguem sem efeito nessas execuções.",
		},
	}
}

func stageAddedTriggerDefinition() NodeDefinition {
	return NodeDefinition{
		Type:          NodeTypeTriggerStageAdded,
		Category:      NodeCategoryTrigger,
		TestBehavior:  NodeTestTrigger,
		Scopes:        []NodeScope{NodeScopeWhatsApp},
		Label:         "Entrou na etapa",
		Description:   "Dispara quando a conversa entra em uma etapa do funil.",
		Icon:          "Kanban",
		DefaultConfig: map[string]interface{}{DataKeyStageID: ""},
		ConfigSchema: []ConfigField{
			{Key: DataKeyStageID, Label: "Etapa", Type: "select", OptionsSource: "stages", Description: "Vazio: qualquer etapa."},
		},
		OutputKeys: []OutputKeyDefinition{
			{Key: DataKeyStageID, Description: "ID da etapa em que a conversa entrou"},
		},
		Guidance: NodeGuidance{
			When: "Para agir quando a conversa muda de etapa no funil, por exemplo avisar a equipe ou mandar um template de acompanhamento.",
			Behavior: "Disponibiliza {{stage_id}}. Sem etapa escolhida, dispara para qualquer etapa. Vale para toda entrada em etapa: CRM, inbox, Elo, agente de IA, outro fluxo e a etapa inicial de uma conversa nova. " +
				"Uma etapa mudada por este mesmo fluxo não o inicia de novo, e uma sequência de fluxos movendo a conversa para depois de " + strconv.Itoa(MaxStageChain) + " fluxos seguidos. " +
				"Enquanto este fluxo está em execução na conversa, uma nova mudança de etapa não inicia outra execução. A conversa pode estar fora da janela de atendimento: comece com um template.",
		},
	}
}
