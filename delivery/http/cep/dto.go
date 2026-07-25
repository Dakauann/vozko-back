package cep

type CEPSearchRequest struct {
	CEP string `json:"cep" validate:"required" example:"01310-100"`
}

type CEPResponse struct {
	Cep        string `json:"cep" example:"01310-100"`
	Logradouro string `json:"logradouro" example:"Avenida Paulista"`
	Complement string `json:"complemento" example:"lado ímpar"`
	Bairro     string `json:"bairro" example:"Bela Vista"`
	Localidade string `json:"localidade" example:"São Paulo"`
	Uf         string `json:"uf" example:"SP"`
	Erro       bool   `json:"erro,omitempty"`
}
