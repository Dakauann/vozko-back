package user

type PlanResponse struct {
	Name            string `json:"name" example:"Profissional"`
	MaxCallChannels int    `json:"maxCallChannels" example:"5"`
	MaxBranches     int    `json:"maxBranches" example:"10"`
}

type UserInfoResponse struct {
	ID            string        `json:"id" example:"usr_a1b2c3"`
	Username      string        `json:"username" example:"maria.silva"`
	Email         string        `json:"email" example:"mari****@exemplo.com"`
	Picture       string        `json:"picture,omitempty" example:"https://cdn.exemplo.com/avatares/maria.png"`
	Role          string        `json:"role" example:"user"`
	CustomerType  string        `json:"customerType" example:"individual"`
	EmailVerified bool          `json:"emailVerified"`
	HasDocument   bool          `json:"hasDocument"`
	Document      string        `json:"document,omitempty" example:"***.***.**9-24"`
	CreatedAt     string        `json:"createdAt" example:"2026-01-15T10:30:00Z"`
	UpdatedAt     string        `json:"updatedAt" example:"2026-02-20T14:45:00Z"`
	Plan          *PlanResponse `json:"plan,omitempty"`
}

type MessageResponse struct {
	Message string `json:"message" example:"Conta excluída com sucesso"`
}

type UpdateProfileRequest struct {
	Username string `json:"username" example:"maria.silva"`
	Picture  string `json:"picture" example:"https://cdn.exemplo.com/avatares/maria.png"`
	Document string `json:"document" example:"123.456.789-09"`
}

type DeleteMeRequest struct {
	CurrentPassword string `json:"currentPassword" example:"MinhaSenh@Atual"`
}

type UserListItemResponse struct {
	ID            string `json:"id"`
	Username      string `json:"username"`
	Email         string `json:"email"`
	Picture       string `json:"picture,omitempty"`
	Role          string `json:"role"`
	CustomerType  string `json:"customerType"`
	EmailVerified bool   `json:"emailVerified"`
	CreatedAt     string `json:"createdAt"`
	UpdatedAt     string `json:"updatedAt"`
}

type UserDetailResponse struct {
	ID            string `json:"id"`
	Username      string `json:"username"`
	Email         string `json:"email"`
	Picture       string `json:"picture,omitempty"`
	Role          string `json:"role"`
	CustomerType  string `json:"customerType"`
	CPF           string `json:"cpf,omitempty"`
	CNPJ          string `json:"cnpj,omitempty"`
	EmailVerified bool   `json:"emailVerified"`
	CreatedAt     string `json:"createdAt"`
	UpdatedAt     string `json:"updatedAt"`
}

type updateRoleRequest struct {
	Role string `json:"role"`
}
