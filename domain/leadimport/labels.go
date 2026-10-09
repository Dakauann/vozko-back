package leadimport

import (
	"errors"
	"strings"

	"vozko/domain/lead"
)

type reasonLabels struct {
	pt, en, es, de string
}

var labels = map[lead.RejectReason]reasonLabels{
	lead.ReasonInvalid:                  {"Número de WhatsApp inválido", "Invalid WhatsApp number", "Número de WhatsApp no válido", "Ungültige WhatsApp-Nummer"},
	lead.ReasonDuplicate:                {"Número repetido no arquivo", "Number repeated in the file", "Número repetido en el archivo", "Nummer in der Datei wiederholt"},
	lead.ReasonIdentityRequired:         {"Sem número de WhatsApp e sem nome", "No WhatsApp number and no name", "Sin número de WhatsApp ni nombre", "Weder WhatsApp-Nummer noch Name"},
	lead.ReasonNameTooLong:              {"Nome longo demais", "Name too long", "Nombre demasiado largo", "Name zu lang"},
	lead.ReasonNicknameTooLong:          {"Apelido longo demais", "Nickname too long", "Apodo demasiado largo", "Spitzname zu lang"},
	lead.ReasonEmailInvalid:             {"E-mail inválido", "Invalid e-mail", "Correo electrónico no válido", "Ungültige E-Mail"},
	lead.ReasonBirthDateInvalid:         {"Data de nascimento inválida", "Invalid birth date", "Fecha de nacimiento no válida", "Ungültiges Geburtsdatum"},
	lead.ReasonPhoneInvalid:             {"Telefone de contato inválido", "Invalid contact phone", "Teléfono de contacto no válido", "Ungültige Kontaktnummer"},
	lead.ReasonPhoneLimit:               {"O lead já tem o máximo de telefones", "The lead already has the most phones allowed", "El lead ya tiene el máximo de teléfonos", "Der Lead hat bereits die Höchstzahl an Telefonnummern"},
	lead.ReasonAddressInvalid:           {"Endereço sem CEP válido nem cidade com UF", "Address without a valid CEP or a city with state", "Dirección sin CEP válido ni ciudad con estado", "Adresse ohne gültige CEP oder Stadt mit Bundesstaat"},
	lead.ReasonCoordinatesInvalid:       {"Latitude ou longitude inválida", "Invalid latitude or longitude", "Latitud o longitud no válida", "Ungültiger Breiten- oder Längengrad"},
	lead.ReasonCoordinatesOutsideBrazil: {"Coordenadas fora do Brasil", "Coordinates outside Brazil", "Coordenadas fuera de Brasil", "Koordinaten außerhalb Brasiliens"},
	lead.ReasonOwnerNotFound:            {"Responsável não é membro do workspace", "Owner is not a member of the workspace", "El responsable no es miembro del workspace", "Verantwortliche Person ist kein Mitglied des Workspace"},
	lead.ReasonOwnerOutOfReach:          {"Responsável fora das pessoas que você vê", "Owner is outside the people you can see", "Responsable fuera de las personas que puede ver", "Verantwortliche Person liegt außerhalb Ihrer Sichtbarkeit"},
	lead.ReasonConsentDateInvalid:       {"Data do consentimento inválida", "Invalid consent date", "Fecha de consentimiento no válida", "Ungültiges Einwilligungsdatum"},
	lead.ReasonConsentPurposeTooLong:    {"Finalidade do consentimento longa demais", "Consent purpose too long", "Finalidad del consentimiento demasiado larga", "Zweck der Einwilligung zu lang"},
	lead.ReasonCustomFieldInvalid:       {"Valor que o campo personalizado não aceita", "Value the custom field does not accept", "Valor que el campo personalizado no acepta", "Wert, den das benutzerdefinierte Feld nicht akzeptiert"},
	lead.ReasonCustomFieldRequired:      {"Campo personalizado obrigatório vazio", "Required custom field is empty", "Campo personalizado obligatorio vacío", "Pflichtfeld ist leer"},
	lead.ReasonRelativeNumberInvalid:    {"Número do familiar inválido", "Invalid relative number", "Número del familiar no válido", "Ungültige Nummer des Angehörigen"},
	lead.ReasonRelationKindInvalid:      {"Parentesco desconhecido", "Unknown kinship", "Parentesco desconocido", "Unbekannte Verwandtschaft"},
	lead.ReasonRelativeNotFound:         {"Familiar não encontrado no arquivo nem na base", "Relative not found in the file or the base", "Familiar no encontrado en el archivo ni en la base", "Angehörige Person weder in der Datei noch im Bestand gefunden"},
	lead.ReasonRelationSelf:             {"O familiar é a própria pessoa", "The relative is the person themselves", "El familiar es la misma persona", "Die angehörige Person ist die Person selbst"},
	lead.ReasonRelationExists:           {"Os dois já têm um vínculo de família", "The two are already linked as family", "Los dos ya tienen un vínculo familiar", "Die beiden sind bereits als Familie verbunden"},
	lead.ReasonContactAmbiguous:         {"Telefone compartilhado por leads demais para achar a pessoa", "Phone shared by too many leads to find the person", "Teléfono compartido por demasiados leads para encontrar a la persona", "Telefonnummer wird von zu vielen Leads geteilt, um die Person zu finden"},
	lead.ReasonLeadChanging:             {"O lead estava sendo alterado; importe a linha de novo", "The lead was being changed; import the row again", "El lead se estaba modificando; importe la fila de nuevo", "Der Lead wurde gerade geändert; importieren Sie die Zeile erneut"},
	lead.ReasonRecordInvalid:            {"Os dados da linha não formam um cadastro válido", "The row does not make a valid record", "Los datos de la fila no forman un registro válido", "Die Zeile ergibt keinen gültigen Datensatz"},
}

func Reasons() []lead.RejectReason {
	return []lead.RejectReason{
		lead.ReasonInvalid, lead.ReasonDuplicate, lead.ReasonIdentityRequired, lead.ReasonNameTooLong, lead.ReasonNicknameTooLong,
		lead.ReasonEmailInvalid, lead.ReasonBirthDateInvalid, lead.ReasonPhoneInvalid, lead.ReasonPhoneLimit, lead.ReasonAddressInvalid,
		lead.ReasonCoordinatesInvalid, lead.ReasonCoordinatesOutsideBrazil, lead.ReasonOwnerNotFound, lead.ReasonOwnerOutOfReach,
		lead.ReasonConsentDateInvalid, lead.ReasonConsentPurposeTooLong, lead.ReasonCustomFieldInvalid, lead.ReasonCustomFieldRequired,
		lead.ReasonRelativeNumberInvalid, lead.ReasonRelationKindInvalid, lead.ReasonRelativeNotFound, lead.ReasonRelationSelf, lead.ReasonRelationExists, lead.ReasonContactAmbiguous,
		lead.ReasonLeadChanging, lead.ReasonRecordInvalid,
	}
}

func ReasonLabel(reason lead.RejectReason, locale string) string {
	l, ok := labels[reason]
	if !ok {
		return string(reason)
	}
	switch strings.ToLower(strings.TrimSpace(locale)) {
	case "en":
		return l.en
	case "es":
		return l.es
	case "de":
		return l.de
	}
	return l.pt
}

type ColumnLabels struct {
	Line, Reason, Code, Field, Imported, Yes, No string
}

func RejectionColumns(locale string) ColumnLabels {
	switch strings.ToLower(strings.TrimSpace(locale)) {
	case "en":
		return ColumnLabels{"line", "reason", "code", "column", "row imported", "yes", "no"}
	case "es":
		return ColumnLabels{"línea", "motivo", "código", "columna", "fila importada", "sí", "no"}
	case "de":
		return ColumnLabels{"Zeile", "Grund", "Code", "Spalte", "Zeile importiert", "ja", "nein"}
	}
	return ColumnLabels{"linha", "motivo", "código", "coluna", "linha importada", "sim", "não"}
}

var errorCodes = []struct {
	err  error
	code string
}{
	{ErrNotFound, "lead_import_not_found"},
	{ErrRunning, "lead_import_running"},
	{ErrNotReady, "lead_import_not_ready"},
	{ErrFileTooLarge, "lead_import_file_too_large"},
	{ErrTooManyRows, "lead_import_too_many_rows"},
	{ErrFileEmpty, "lead_import_empty"},
	{ErrUnsupportedFile, "lead_import_unsupported_file"},
	{ErrFileUnavailable, "lead_import_file_unavailable"},
	{ErrUnavailable, "lead_import_unavailable"},
	{ErrMappingInvalid, "lead_import_mapping_invalid"},
	{ErrForbidden, "lead_import_forbidden"},
	{ErrClaimLost, "lead_import_claim_lost"},
	{ErrOnlyMine, "lead_import_only_mine"},
}

func ErrorCode(err error) string {
	for _, known := range errorCodes {
		if errors.Is(err, known.err) {
			return known.code
		}
	}
	return ""
}
