package insurance

var policyRequiredFields = map[PolicyType][]RequiredField{
	PolicyTypeJudicialExecutionFiscal: cloneRequiredFields([]RequiredField{
		{Path: "policyPeriodStart", Alias: "Início da Vigência", Type: FieldTypeDate, Description: "Data de início do período da apólice (AAAA-MM-DD)"},
		{Path: "policyPeriodEnd", Alias: "Fim da Vigência", Type: FieldTypeDate, Description: "Data de término do período da apólice (AAAA-MM-DD)"},

		{Path: "participants[]", Alias: "Participantes", Type: FieldTypeArray, Description: "Pelo menos um participante"},
		{Path: "participants[].documentNumber", Alias: "Documento", Type: FieldTypeString, Description: "Número do documento do participante"},
		{Path: "participants[].role", Alias: "Papel", Type: FieldTypeString, Description: "Função do participante", AllowedValues: []string{"Beneficiary", "Insured", "PolicyHolder"}, AllowedValuesAliases: map[string]string{"Beneficiary": "Beneficiário", "Insured": "Segurado", "PolicyHolder": "Titular da Apólice"}},
		{Path: "riskObjects[]", Alias: "Objetos de Risco", Type: FieldTypeArray, Description: "Pelo menos um objeto de risco"},
		{Path: "riskObjects[].coverages[]", Alias: "Coberturas", Type: FieldTypeArray, Description: "Pelo menos uma cobertura por objeto de risco", Optional: true},
		{Path: "riskObjects[].coverages[].key", Alias: "Tipo de Cobertura", Type: FieldTypeString, Description: "Identificador da cobertura"},
		{Path: "riskObjects[].coverages[].insuredAmount", Alias: "Valor Segurado", Type: FieldTypeNumber, Description: "Valor segurado da cobertura"},
		{Path: "riskObjects[].managementModality", Alias: "Modalidade de Gestão", Type: FieldTypeString, Description: "Modalidade de gestão", AllowedValues: []string{
			"AnnulmentAction",
			"DeclarationNullification",
			"DeclaratoryActionLackOfDebit",
			"EmbargoesOnExecution",
			"InjunctiveRelief",
			"OrdinaryAction",
			"PrecautionaryAction",
			"PreventiveInjunction",
			"TaxEnforcement",
		}, AllowedValuesAliases: map[string]string{
			"AnnulmentAction":              "Ação Anulatória",
			"DeclarationNullification":     "Declaração de Nulidade",
			"DeclaratoryActionLackOfDebit": "Ação Declaratória de Inexistência de Débito",
			"EmbargoesOnExecution":         "Embargos à Execução",
			"InjunctiveRelief":             "Tutela Provisória",
			"OrdinaryAction":               "Ação Ordinária",
			"PrecautionaryAction":          "Ação Cautelar",
			"PreventiveInjunction":         "Medida Preventiva",
			"TaxEnforcement":               "Execução Fiscal",
		}},
		{Path: "riskObjects[].taxCharged", Alias: "Imposto Cobrado", Type: FieldTypeString, Description: "Descrição do imposto cobrado"},
		{Path: "riskObjects[].documentValidityPeriod", Alias: "Período de Validade", Type: FieldTypeNumber, Description: "Período de validade do documento em anos"},
	}),
	PolicyTypeImobiliario: cloneRequiredFields([]RequiredField{
		{Path: "policyPeriodStart", Alias: "Início da Vigência", Type: FieldTypeDate, Description: "Data de início do período da apólice (AAAA-MM-DD)", Optional: true},
		{Path: "policyPeriodEnd", Alias: "Fim da Vigência", Type: FieldTypeDate, Description: "Data de término do período da apólice (AAAA-MM-DD)", Optional: true},

		{Path: "participants[]", Alias: "Participantes", Type: FieldTypeArray, Description: "Pelo menos um participante"},
		{Path: "participants[].documentNumber", Alias: "Documento", Type: FieldTypeString, Description: "Número do documento do participante"},
		{Path: "participants[].role", Alias: "Papel", Type: FieldTypeString, Description: "Função do participante", AllowedValues: []string{"Beneficiary", "Insured", "PolicyHolder"}, AllowedValuesAliases: map[string]string{"Beneficiary": "Beneficiário", "Insured": "Segurado", "PolicyHolder": "Titular da Apólice"}},
		{Path: "riskObjects[]", Alias: "Objetos de Risco", Type: FieldTypeArray, Description: "Pelo menos um objeto de risco de propriedade"},
		{Path: "riskObjects[].type", Alias: "Tipo", Type: FieldTypeString, Description: "Tipo de objeto de risco", AllowedValues: []string{"Property"}, AllowedValuesAliases: map[string]string{"Property": "Propriedade"}},
		{Path: "riskObjects[].coverages[]", Alias: "Coberturas", Type: FieldTypeArray, Description: "Pelo menos uma cobertura"},
		{Path: "riskObjects[].coverages[].key", Alias: "Tipo de Cobertura", Type: FieldTypeString, Description: "Identificador da cobertura", AllowedValues: []string{
			"basica",
			"danos-eletricos",
			"impacto-veiculos",
			"perda-aluguel",
			"quebra-vidros",
			"rc-familiar",
			"roubo-furto",
			"ruptura",
			"vendaval",
		}, AllowedValuesAliases: map[string]string{
			"basica":           "Básica",
			"danos-eletricos":  "Danos Elétricos",
			"impacto-veiculos": "Impacto de Veículos",
			"perda-aluguel":    "Perda de Aluguel",
			"quebra-vidros":    "Quebra de Vidros",
			"rc-familiar":      "Responsabilidade Civil Familiar",
			"roubo-furto":      "Roubo e Furto",
			"ruptura":          "Ruptura",
			"vendaval":         "Vendaval",
		}},
		{Path: "riskObjects[].coverages[].insuredAmount", Alias: "Valor Segurado", Type: FieldTypeNumber, Description: "Valor segurado da cobertura"},
		{Path: "riskObjects[].historicalProtectedProperty", Alias: "Imóvel Tombado", Type: FieldTypeBoolean, Description: "Se o imóvel é protegido historicamente"},
		{Path: "riskObjects[].sharedProperty", Alias: "Imóvel Compartilhado", Type: FieldTypeBoolean, Description: "Se o imóvel é compartilhado"},
		{Path: "riskObjects[].insuredOwner", Alias: "Segurado é Proprietário", Type: FieldTypeBoolean, Description: "Se o segurado é proprietário do imóvel"},
		{Path: "riskObjects[].propertyType", Alias: "Tipo de Imóvel", Type: FieldTypeString, Description: "Tipo de imóvel", AllowedValues: []string{
			"Apartment",
			"CondominiumApartment",
			"House",
			"CondominiumHouse",
			"CountrySideHouse",
		}, AllowedValuesAliases: map[string]string{
			"Apartment":            "Apartamento",
			"CondominiumApartment": "Apartamento em Condomínio",
			"House":                "Casa",
			"CondominiumHouse":     "Casa em Condomínio",
			"CountrySideHouse":     "Casa de Campo",
		}},
		{Path: "riskObjects[].constructionType", Alias: "Tipo de Construção", Type: FieldTypeString, Description: "Tipo de construção", AllowedValues: []string{"Brick", "Wood", "WoodAndBrick"}, AllowedValuesAliases: map[string]string{"Brick": "Alvenaria", "Wood": "Madeira", "WoodAndBrick": "Mista (Madeira e Alvenaria)"}},
		{Path: "riskObjects[].propertyUseType", Alias: "Tipo de Uso", Type: FieldTypeString, Description: "Tipo de uso do imóvel", AllowedValues: []string{"Usual", "Summer", "Vacate"}, AllowedValuesAliases: map[string]string{"Usual": "Habitual", "Summer": "Veraneio", "Vacate": "Desocupado"}},
		{Path: "riskObjects[].address", Alias: "Endereço", Type: FieldTypeObject, Description: "Endereço do imóvel"},
		{Path: "riskObjects[].address.street", Alias: "Rua", Type: FieldTypeString, Description: "Rua do imóvel"},
		{Path: "riskObjects[].address.number", Alias: "Número", Type: FieldTypeString, Description: "Número do imóvel"},
		{Path: "riskObjects[].address.district", Alias: "Bairro", Type: FieldTypeString, Description: "Bairro do imóvel"},
		{Path: "riskObjects[].address.city", Alias: "Cidade", Type: FieldTypeString, Description: "Cidade do imóvel"},
		{Path: "riskObjects[].address.state", Alias: "Estado", Type: FieldTypeString, Description: "Estado do imóvel"},
		{Path: "riskObjects[].address.zipCode", Alias: "CEP", Type: FieldTypeString, Description: "CEP do imóvel"},
	}),
}

func RequiredFieldsForPolicy(policy PolicyType) ([]RequiredField, error) {
	fields, ok := policyRequiredFields[policy]
	if !ok {
		return nil, ErrUnsupportedPolicyType
	}
	return cloneRequiredFields(fields), nil
}

func cloneRequiredFields(fields []RequiredField) []RequiredField {
	if len(fields) == 0 {
		return nil
	}
	out := make([]RequiredField, len(fields))
	for i, field := range fields {
		copyField := field
		if len(field.Providers) > 0 {
			copyField.Providers = append([]InsuranceProvider(nil), field.Providers...)
		}
		if len(field.AllowedValues) > 0 {
			copyField.AllowedValues = append([]string(nil), field.AllowedValues...)
		}
		if len(field.AllowedValuesAliases) > 0 {
			copyField.AllowedValuesAliases = make(map[string]string, len(field.AllowedValuesAliases))
			for k, v := range field.AllowedValuesAliases {
				copyField.AllowedValuesAliases[k] = v
			}
		}
		out[i] = copyField
	}
	return out
}
