package issue

type CreateIssueRequest struct {
	Title       string   `json:"title" example:"Erro ao enviar mensagem"`
	Description string   `json:"description" example:"Ao clicar em enviar, a tela trava."`
	ImageURLs   []string `json:"imageUrls,omitempty" example:"https://cdn.example.com/prints/erro.png"`
}

type UpdateIssueStatusRequest struct {
	Status string `json:"status" example:"in_progress"`
}

type CreateIssueResponseRequest struct {
	Body     string  `json:"body" example:"Estamos analisando o problema."`
	ImageURL *string `json:"imageUrl,omitempty" example:"https://cdn.example.com/prints/anexo.png"`
}
