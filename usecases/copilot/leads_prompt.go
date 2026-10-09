package copilot_usecase

import (
	"strconv"
	"strings"

	"vozko/domain/copilot"
	"vozko/domain/crmfilter"
)

var leadFilterFieldNames = map[crmfilter.Field]string{
	crmfilter.FieldQuery:          "busca",
	crmfilter.FieldName:           "nome",
	crmfilter.FieldNumber:         "número",
	crmfilter.FieldPhoneAny:       "telefone",
	crmfilter.FieldCity:           "cidade",
	crmfilter.FieldDistrict:       "bairro",
	crmfilter.FieldState:          "UF",
	crmfilter.FieldZip:            "CEP",
	crmfilter.FieldArea:           "área desenhada no mapa",
	crmfilter.FieldOwner:          "responsável",
	crmfilter.FieldCustom:         "campo personalizado",
	crmfilter.FieldBlocked:        "bloqueio",
	crmfilter.FieldOptedOut:       "não quer receber mensagens",
	crmfilter.FieldWindowOpen:     "janela aberta",
	crmfilter.FieldCampaign:       "campanha",
	crmfilter.FieldChannel:        "canal",
	crmfilter.FieldStage:          "etapa",
	crmfilter.FieldLabel:          "etiqueta",
	crmfilter.FieldMemoryCategory: "memórias",
	crmfilter.FieldBirthday:       "aniversário",
	crmfilter.FieldCreatedAt:      "data de cadastro",
	crmfilter.FieldLastActivityAt: "última atividade",
}

func leadsScreenPrompt(view copilot.View) string {
	var b strings.Builder
	b.WriteString("\n\n# Tela atual\nO usuário está na página Leads.\n")
	fields := view.LeadFilterFields()
	if len(fields) == 0 {
		b.WriteString("- filtro da tela: sem filtro (todos os leads do workspace)\n")
	} else {
		names := make([]string, 0, len(fields))
		for _, field := range fields {
			names = append(names, leadFilterFieldName(field))
		}
		b.WriteString("- filtro da tela: " + strings.Join(names, ", ") + " (os valores não aparecem aqui)\n")
	}
	b.WriteString("- leads marcados na tabela: " + strconv.Itoa(view.SelectedLeads) + "\n")
	b.WriteString(`"Esses leads" e "aqui" se referem ao filtro da tela: passe use_screen_filter=true em search_leads,
lead_geo_summary e prepare_lead_action para partir dele. Os leads marcados na tabela não chegam até você; para agir
só sobre eles, peça ao usuário para usar a barra de ações da tabela.`)
	return b.String()
}

func leadFilterFieldName(field crmfilter.Field) string {
	if name, ok := leadFilterFieldNames[field]; ok {
		return name
	}
	return string(field)
}
