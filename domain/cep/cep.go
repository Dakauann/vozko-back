package cep

type CEPInfo struct {
	Cep        string `json:"cep"`
	Logradouro string `json:"logradouro"`
	Complement string `json:"complemento"`
	Bairro     string `json:"bairro"`
	Localidade string `json:"localidade"`
	Uf         string `json:"uf"`
	Erro       bool   `json:"erro,omitempty"`
}

type CEPRepository interface {
	GetByCode(cepCode string) (*CEPInfo, error)
	Create(cep *CEPInfo) error
}

type CEPSearchUseCase interface {
	Execute(cep string) (*CEPInfo, error)
}
