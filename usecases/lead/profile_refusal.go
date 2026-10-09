package lead_usecase

import (
	"errors"
	"fmt"

	"vozko/domain/address"
	"vozko/domain/customfield"
	"vozko/domain/lead"
	"vozko/domain/shared"
)

func ExplainProfileRefusal(err error) (string, bool) {
	var invalidPart address.InvalidFieldError
	var valueErr *customfield.ValueError
	switch {
	case errors.Is(err, lead.ErrProfileEmpty):
		return "Nenhum dado para gravar: informe CEP, endereço, bairro, data de nascimento ou um campo personalizado.", true
	case errors.Is(err, lead.ErrProfileCEPUnchecked):
		return "A consulta de CEP está indisponível no momento; nada foi salvo. Tente novamente em alguns minutos.", true
	case errors.Is(err, lead.ErrProfileCEPUnknown):
		return "CEP não encontrado; confirme o CEP antes de tentar de novo.", true
	case errors.Is(err, address.ErrCEPMismatch):
		return "A cidade ou a UF informada não pertence a esse CEP; confirme o CEP ou a cidade.", true
	case errors.As(err, &invalidPart):
		return addressRefusal(invalidPart), true
	case errors.Is(err, lead.ErrLeadBirthDateInvalid):
		return "A data de nascimento não é válida: use DD/MM/AAAA, com uma data no passado.", true
	case errors.As(err, &valueErr):
		return fieldRefusal(valueErr), true
	case errors.Is(err, lead.ErrLeadNotFound):
		return "A conversa não tem lead vinculado ou o lead não existe mais; o cadastro não foi atualizado.", true
	case errors.Is(err, shared.ErrVersionConflict):
		return "O cadastro do lead estava sendo alterado ao mesmo tempo; nada foi sobrescrito, tente novamente.", true
	}
	return "", false
}

func addressRefusal(e address.InvalidFieldError) string {
	switch {
	case e.Rule == address.RuleZipOrCityRequired:
		return "Endereço incompleto: informe o CEP ou a cidade e UF."
	case e.Field == address.FieldZipCode:
		return "CEP inválido: use 8 dígitos, ex.: 01310-100."
	case e.Field == address.FieldState:
		return "UF inválida: use a sigla do estado com duas letras, ex.: SP."
	case e.Field == address.FieldCityCode:
		return "Código IBGE do município inválido: use 7 dígitos."
	case e.Rule == address.RuleTooLong:
		return fmt.Sprintf("O valor de %s é longo demais; resuma.", e.Field)
	}
	return "Endereço inválido; confira os dados informados."
}

func fieldRefusal(e *customfield.ValueError) string {
	switch {
	case errors.Is(e.Err, customfield.ErrUnknownKey):
		return fmt.Sprintf("O campo %q não existe nos campos de lead deste workspace.", e.Key)
	case errors.Is(e.Err, customfield.ErrValueForbidden):
		return fmt.Sprintf("O campo %q é sensível e nunca é gravado por uma automação.", e.Key)
	case errors.Is(e.Err, customfield.ErrValueRequired):
		return fmt.Sprintf("O campo obrigatório %q está vazio no cadastro; um atendente precisa completá-lo antes.", e.Key)
	}
	return fmt.Sprintf("Valor inválido para o campo %q.", e.Key)
}
