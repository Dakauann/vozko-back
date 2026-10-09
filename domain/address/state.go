package address

import (
	"strings"

	"vozko/domain/shared"
)

var stateNames = map[string]string{
	"acre": "AC", "alagoas": "AL", "amapa": "AP", "amazonas": "AM", "bahia": "BA", "ceara": "CE",
	"distrito federal": "DF", "espirito santo": "ES", "goias": "GO", "maranhao": "MA", "mato grosso": "MT",
	"mato grosso do sul": "MS", "minas gerais": "MG", "para": "PA", "paraiba": "PB", "parana": "PR",
	"pernambuco": "PE", "piaui": "PI", "rio de janeiro": "RJ", "rio grande do norte": "RN",
	"rio grande do sul": "RS", "rondonia": "RO", "roraima": "RR", "santa catarina": "SC",
	"sao paulo": "SP", "sergipe": "SE", "tocantins": "TO",
}

func StateCode(raw string) (string, bool) {
	text := collapse(raw)
	if code := strings.ToUpper(text); brazilStates[code] {
		return code, true
	}
	code, ok := stateNames[shared.FoldForMatch(text)]
	return code, ok
}

type IBGEState struct {
	Code  string
	State string
}

var ibgeStates = []IBGEState{
	{"11", "RO"}, {"12", "AC"}, {"13", "AM"}, {"14", "RR"}, {"15", "PA"}, {"16", "AP"}, {"17", "TO"},
	{"21", "MA"}, {"22", "PI"}, {"23", "CE"}, {"24", "RN"}, {"25", "PB"}, {"26", "PE"}, {"27", "AL"},
	{"28", "SE"}, {"29", "BA"}, {"31", "MG"}, {"32", "ES"}, {"33", "RJ"}, {"35", "SP"}, {"41", "PR"},
	{"42", "SC"}, {"43", "RS"}, {"50", "MS"}, {"51", "MT"}, {"52", "GO"}, {"53", "DF"},
}

func IBGEStates() []IBGEState { return append([]IBGEState(nil), ibgeStates...) }

func StateOfCityCode(code string) (string, bool) {
	if !ValidCityCode(code) {
		return "", false
	}
	for _, s := range ibgeStates {
		if strings.HasPrefix(code, s.Code) {
			return s.State, true
		}
	}
	return "", false
}
