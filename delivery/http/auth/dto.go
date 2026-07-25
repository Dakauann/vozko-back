package auth

import "time"

type LoginRequest struct {
	Email    string `json:"email" validate:"required" example:"contato@clinica.com.br"`
	Password string `json:"password" validate:"required" example:"SenhaForte1"`
}

type RegisterRequest struct {
	Name              string `json:"name" validate:"required" example:"Clínica Sorriso"`
	Email             string `json:"email" validate:"required" example:"contato@clinica.com.br"`
	Password          string `json:"password" validate:"required" example:"SenhaForte1"`
	CustomerType      string `json:"customerType" validate:"required" enums:"individual,company" example:"company"`
	CPF               string `json:"cpf" example:"12345678901"`
	CNPJ              string `json:"cnpj" example:"12345678000190"`
	VerificationToken string `json:"verificationToken" validate:"required" example:"123456"`
	ReferralCode      string `json:"referralCode" example:"PARCEIRO10"`
}

type AdminRegisterRequest struct {
	Name         string `json:"name" validate:"required" example:"Clínica Sorriso"`
	Email        string `json:"email" validate:"required" example:"contato@clinica.com.br"`
	Password     string `json:"password" validate:"required" example:"SenhaForte1"`
	CustomerType string `json:"customerType" validate:"required" enums:"individual,company" example:"company"`
	CPF          string `json:"cpf" example:"12345678901"`
	CNPJ         string `json:"cnpj" example:"12345678000190"`
}

type RefreshTokenRequest struct {
	RefreshToken string `json:"refreshToken" example:"rt_a1b2c3d4"`
}

type ForgotPasswordRequest struct {
	Email string `json:"email" validate:"required" example:"contato@clinica.com.br"`
}

type ResetPasswordRequest struct {
	Email       string `json:"email" validate:"required" example:"contato@clinica.com.br"`
	Token       string `json:"token" validate:"required" example:"reset_a1b2c3d4"`
	NewPassword string `json:"newPassword" validate:"required" example:"NovaSenhaForte1"`
}

type SendEmailVerificationRequest struct {
	Email string `json:"email" validate:"required" example:"contato@clinica.com.br"`
}

type VerifyEmailRequest struct {
	Token string `json:"token" validate:"required" example:"123456"`
}

type ChangePasswordRequest struct {
	CurrentPassword string `json:"currentPassword" validate:"required" example:"SenhaForte1"`
	NewPassword     string `json:"newPassword" validate:"required" example:"NovaSenhaForte1"`
}

type LogoutRequest struct {
	SessionID string `json:"sessionId" example:"sess_a1b2c3d4"`
}

type AuthTokenResponse struct {
	AccessToken  string `json:"accessToken,omitempty" example:"eyJhbGciOi..."`
	RefreshToken string `json:"refreshToken,omitempty" example:"rt_a1b2c3d4"`
	TokenType    string `json:"tokenType" example:"Bearer"`
	UserID       string `json:"userId" example:"usr_a1b2c3"`
	Email        string `json:"email" example:"contato@clinica.com.br"`
	Name         string `json:"name" example:"Clínica Sorriso"`
	Picture      string `json:"picture" example:"https://cdn.example.com/avatars/usr_a1b2c3.png"`
	Role         string `json:"role" example:"user"`
	CustomerType string `json:"customerType" example:"company"`
}

type AdminRegisterResponse struct {
	UserID       string `json:"userId" example:"usr_a1b2c3"`
	Email        string `json:"email" example:"contato@clinica.com.br"`
	Name         string `json:"name" example:"Clínica Sorriso"`
	Role         string `json:"role" example:"user"`
	CustomerType string `json:"customerType" example:"company"`
	Message      string `json:"message" example:"User registered successfully"`
}

type MessageResponse struct {
	Message string `json:"message" example:"Operation completed successfully"`
}

type EmailVerificationQueuedResponse struct {
	Message string `json:"message" example:"Verification email queued"`
	Success bool   `json:"success" example:"true"`
}

type SessionResponse struct {
	ID         string    `json:"id" example:"sess_a1b2c3d4"`
	DeviceInfo string    `json:"deviceInfo" example:"Mozilla/5.0 (Windows NT 10.0; Win64; x64)"`
	IPAddress  string    `json:"ipAddress" example:"200.150.10.20"`
	Location   string    `json:"location" example:"São Paulo, BR"`
	IsCurrent  bool      `json:"isCurrent" example:"true"`
	CreatedAt  time.Time `json:"createdAt"`
	ExpiresAt  time.Time `json:"expiresAt"`
}
