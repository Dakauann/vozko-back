package document

import (
	"strings"
	"unicode"

	"vozko/domain/customer"
)

type validator struct{}

func NewValidator() customer.DocumentValidator {
	return &validator{}
}

func (v *validator) ValidateCPFOrCNPJ(document string) bool {
	digits := onlyDigits(document)
	switch len(digits) {
	case 11:
		return validateCPF(digits)
	case 14:
		return validateCNPJ(digits)
	default:
		return false
	}
}

func (v *validator) Normalize(document string) string {
	return onlyDigits(document)
}

func onlyDigits(input string) string {
	var b strings.Builder
	for _, r := range input {
		if unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func validateCPF(cpf string) bool {
	if len(cpf) != 11 {
		return false
	}

	allEqual := true
	for i := 1; i < len(cpf); i++ {
		if cpf[i] != cpf[0] {
			allEqual = false
			break
		}
	}
	if allEqual {
		return false
	}

	sum := 0
	for i := 0; i < 9; i++ {
		sum += int(cpf[i]-'0') * (10 - i)
	}
	firstDigit := (sum * 10) % 11
	if firstDigit == 10 {
		firstDigit = 0
	}
	if firstDigit != int(cpf[9]-'0') {
		return false
	}

	sum = 0
	for i := 0; i < 10; i++ {
		sum += int(cpf[i]-'0') * (11 - i)
	}
	secondDigit := (sum * 10) % 11
	if secondDigit == 10 {
		secondDigit = 0
	}

	return secondDigit == int(cpf[10]-'0')
}

func validateCNPJ(cnpj string) bool {
	if len(cnpj) != 14 {
		return false
	}

	allEqual := true
	for i := 1; i < len(cnpj); i++ {
		if cnpj[i] != cnpj[0] {
			allEqual = false
			break
		}
	}
	if allEqual {
		return false
	}

	weightsFirst := []int{5, 4, 3, 2, 9, 8, 7, 6, 5, 4, 3, 2}
	weightsSecond := []int{6, 5, 4, 3, 2, 9, 8, 7, 6, 5, 4, 3, 2}

	sum := 0
	for i := 0; i < 12; i++ {
		sum += int(cnpj[i]-'0') * weightsFirst[i]
	}
	firstDigit := sum % 11
	if firstDigit < 2 {
		firstDigit = 0
	} else {
		firstDigit = 11 - firstDigit
	}
	if firstDigit != int(cnpj[12]-'0') {
		return false
	}

	sum = 0
	for i := 0; i < 13; i++ {
		sum += int(cnpj[i]-'0') * weightsSecond[i]
	}
	secondDigit := sum % 11
	if secondDigit < 2 {
		secondDigit = 0
	} else {
		secondDigit = 11 - secondDigit
	}

	return secondDigit == int(cnpj[13]-'0')
}
