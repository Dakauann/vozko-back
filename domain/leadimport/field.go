package leadimport

import (
	"strings"

	"vozko/domain/customfield"
	"vozko/domain/lead"
	"vozko/domain/sheet"
	"vozko/domain/workspace"
)

const (
	FieldNumber         = lead.ImportFieldNumber
	FieldPhoneMobile    = "phone:mobile"
	FieldPhoneLandline  = "phone:landline"
	FieldPhoneWork      = "phone:work"
	FieldPhoneMessage   = "phone:message"
	FieldPhoneOther     = "phone:other"
	FieldName           = lead.ImportFieldName
	FieldNickname       = lead.ImportFieldNickname
	FieldEmail          = lead.ImportFieldEmail
	FieldBirthDate      = lead.ImportFieldBirthDate
	FieldZipCode        = "zip_code"
	FieldStreet         = "street"
	FieldStreetNumber   = "street_number"
	FieldComplement     = "complement"
	FieldDistrict       = "district"
	FieldCity           = "city"
	FieldState          = "state"
	FieldLatitude       = "latitude"
	FieldLongitude      = "longitude"
	FieldOwnerEmail     = lead.ImportFieldOwner
	FieldConsentDate    = lead.ImportFieldConsentDate
	FieldConsentPurpose = lead.ImportFieldConsentPurpose
	FieldRelativeNumber = lead.ImportFieldRelative
	FieldRelationKind   = lead.ImportFieldRelationKind

	phoneFieldPrefix = "phone:"
	phonesGroup      = "phones"
)

type Group string

const (
	GroupIdentity Group = "identity"
	GroupPhones   Group = "phones"
	GroupContact  Group = "contact"
	GroupAddress  Group = "address"
	GroupOwner    Group = "owner"
	GroupConsent  Group = "consent"
	GroupCustom   Group = "custom"
	GroupFamily   Group = "family"
)

type Field struct {
	Key        string
	Group      Group
	Label      string
	Requires   workspace.Action
	Sensitive  bool
	Repeatable bool
	aliases    []string
}

var builtInFields = []Field{
	{Key: FieldNumber, Group: GroupIdentity, aliases: []string{"whatsapp", "whats", "wpp", "zap", "telefone", "telefone principal", "whatsapp principal",
		"celular principal", "celular", "phone", "fone", "tel", "contato", "numero do whatsapp", "numero de telefone", "numero do telefone",
		"numero de celular", "numero do celular", "mobile"}},
	{Key: FieldPhoneMobile, Group: GroupPhones, Repeatable: true, aliases: []string{"celular", "cel", "telefone celular", "whatsapp", "movel", "mobile"}},
	{Key: FieldPhoneLandline, Group: GroupPhones, Repeatable: true, aliases: []string{"telefone fixo", "fixo", "residencial", "telefone residencial",
		"fone casa", "telefone casa", "casa", "fone residencial", "fone fixo"}},
	{Key: FieldPhoneWork, Group: GroupPhones, Repeatable: true, aliases: []string{"comercial", "telefone comercial", "trabalho", "fone trabalho",
		"fone comercial", "telefone trabalho"}},
	{Key: FieldPhoneMessage, Group: GroupPhones, Repeatable: true, aliases: []string{"recado", "telefone recado", "fone recado", "telefone para recado"}},
	{Key: FieldPhoneOther, Group: GroupPhones, Repeatable: true, aliases: []string{"telefone", "fone", "tel", "outro telefone", "telefone alternativo",
		"telefone secundario", "phone"}},
	{Key: FieldName, Group: GroupContact, aliases: []string{"nome", "name", "nome completo", "cliente", "nome do cliente", "pessoa", "full name"}},
	{Key: FieldNickname, Group: GroupContact, aliases: []string{"apelido", "nickname", "como gosta de ser chamado", "como e chamado"}},
	{Key: FieldEmail, Group: GroupContact, aliases: []string{"email", "e mail", "correio eletronico", "endereco de email"}},
	{Key: FieldBirthDate, Group: GroupContact, aliases: []string{"nascimento", "data de nascimento", "data nasc", "dt nasc", "dt nascimento",
		"aniversario", "data aniversario", "birthday", "birth date", "birthdate", "data nascimento", "nasc"}},
	{Key: FieldZipCode, Group: GroupAddress, Requires: workspace.ActionReadAddresses, aliases: []string{"cep", "codigo postal", "zip", "zipcode", "zip code", "postal code"}},
	{Key: FieldStreet, Group: GroupAddress, Requires: workspace.ActionReadAddresses, aliases: []string{"endereco", "logradouro", "rua", "street", "address", "avenida"}},
	{Key: FieldStreetNumber, Group: GroupAddress, Requires: workspace.ActionReadAddresses, aliases: []string{"numero da casa", "numero residencial", "num",
		"n", "nº", "no", "numero do endereco", "house number", "street number"}},
	{Key: FieldComplement, Group: GroupAddress, Requires: workspace.ActionReadAddresses, aliases: []string{"complemento", "compl", "apto", "apartamento", "bloco"}},
	{Key: FieldDistrict, Group: GroupAddress, Requires: workspace.ActionReadAddresses, aliases: []string{"bairro", "district", "neighborhood", "distrito"}},
	{Key: FieldCity, Group: GroupAddress, Requires: workspace.ActionReadAddresses, aliases: []string{"cidade", "municipio", "city", "localidade"}},
	{Key: FieldState, Group: GroupAddress, Requires: workspace.ActionReadAddresses, aliases: []string{"uf", "estado", "state"}},
	{Key: FieldLatitude, Group: GroupAddress, Requires: workspace.ActionReadAddresses, aliases: []string{"latitude", "lat"}},
	{Key: FieldLongitude, Group: GroupAddress, Requires: workspace.ActionReadAddresses, aliases: []string{"longitude", "lng", "lon", "long"}},
	{Key: FieldOwnerEmail, Group: GroupOwner, Requires: workspace.ActionAssign, aliases: []string{"responsavel", "email do responsavel", "email responsavel",
		"dono", "owner", "vendedor", "consultor"}},
	{Key: FieldConsentDate, Group: GroupConsent, aliases: []string{"consentimento", "data do consentimento", "data consentimento", "opt in", "optin",
		"data opt in", "data do opt in", "aceite", "data do aceite"}},
	{Key: FieldConsentPurpose, Group: GroupConsent, aliases: []string{"finalidade", "finalidade do consentimento", "proposito", "finalidade do opt in"}},
	{Key: FieldRelativeNumber, Group: GroupFamily, Requires: workspace.ActionUpdate, aliases: []string{"familiar de", "parente de", "telefone do familiar",
		"whatsapp do familiar", "familiar"}},
	{Key: FieldRelationKind, Group: GroupFamily, Requires: workspace.ActionUpdate, aliases: []string{"parentesco", "grau de parentesco", "relacao",
		"vinculo", "relacao familiar"}},
}

var bareNumberHeaders = map[string]bool{"numero": true, "number": true}

func Catalog(defs []*customfield.Definition) []Field {
	fields := make([]Field, 0, len(builtInFields)+len(defs))
	fields = append(fields, builtInFields...)
	for _, d := range defs {
		if d == nil {
			continue
		}
		f := Field{Key: lead.ImportCustomField(d.Key), Group: GroupCustom, Label: d.Label, Sensitive: d.Sensitive, aliases: []string{d.Key, d.Label}}
		if d.Sensitive {
			f.Requires = workspace.ActionReadSensitive
		}
		fields = append(fields, f)
	}
	return fields
}

func fieldIndex(defs []*customfield.Definition) map[string]Field {
	index := map[string]Field{}
	for _, f := range Catalog(defs) {
		index[f.Key] = f
	}
	return index
}

func PhoneLabelOf(field string) (lead.PhoneLabel, bool) {
	label, ok := strings.CutPrefix(field, phoneFieldPrefix)
	if !ok {
		return "", false
	}
	l := lead.PhoneLabel(label)
	return l, l.Valid()
}

func Suggest(headers []string, defs []*customfield.Definition) []Column {
	catalog := Catalog(defs)
	targets := make([]sheet.Target, 0, len(catalog))
	for _, f := range catalog {
		t := sheet.Target{Field: f.Key, Aliases: f.aliases}
		if f.Group == GroupPhones {
			t.Group = phonesGroup
		}
		targets = append(targets, t)
	}
	guessed := sheet.Guess(headers, targets, map[string]int{phonesGroup: lead.MaxImportPhones})
	mapped := map[string]bool{}
	for _, field := range guessed {
		mapped[field] = true
	}
	for i, h := range headers {
		if guessed[i] != "" || !bareNumberHeaders[sheet.HeaderKey(h)] {
			continue
		}
		switch {
		case !mapped[FieldNumber]:
			guessed[i], mapped[FieldNumber] = FieldNumber, true
		case !mapped[FieldStreetNumber] && (mapped[FieldStreet] || mapped[FieldZipCode]):
			guessed[i], mapped[FieldStreetNumber] = FieldStreetNumber, true
		}
	}
	columns := make([]Column, len(headers))
	for i, h := range headers {
		columns[i] = Column{Index: i, Header: h, Field: guessed[i]}
	}
	return columns
}
