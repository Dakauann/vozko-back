package address

import (
	"fmt"
	"strings"

	"vozko/domain/cep"
	"vozko/domain/shared"
)

var ErrCEPMismatch = fmt.Errorf("%w: the city, state or city code does not belong to the CEP", ErrInvalidAddress)

func (p Postal) CompletedBy(info cep.CEPInfo) (Postal, error) {
	given := p.Normalize()
	code, err := cep.Parse(info.Cep)
	if err != nil || given.ZipCode != code {
		return Postal{}, ErrCEPMismatch
	}
	city, state, cityCode := collapse(info.Localidade), strings.ToUpper(collapse(info.Uf)), collapse(info.IBGE)
	if given.City != "" && shared.FoldForMatch(given.City) != shared.FoldForMatch(city) {
		return Postal{}, ErrCEPMismatch
	}
	if given.State != "" {
		if named, _ := StateCode(given.State); named != state {
			return Postal{}, ErrCEPMismatch
		}
	}
	if given.CityCode != "" && cityCode != "" && given.CityCode != cityCode {
		return Postal{}, ErrCEPMismatch
	}
	completed := given
	completed.City, completed.State = city, state
	if cityCode != "" {
		completed.CityCode = cityCode
	}
	if completed.Street == "" {
		completed.Street = collapse(info.Logradouro)
	}
	if completed.District == "" {
		completed.District = collapse(info.Bairro)
	}
	return completed.Normalize(), nil
}
