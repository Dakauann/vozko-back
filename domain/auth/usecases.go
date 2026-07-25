package auth

type CredentialsLoginUseCase interface {
	Execute(input CredentialsInput) (*TokenPair, error)
}

type RegisterUseCase interface {
	Execute(input CredentialsInput) (*TokenPair, error)
}

type AdminRegisterUseCase interface {
	Execute(input CredentialsInput) (*TokenPair, error)
}

type RefreshTokenUseCase interface {
	Execute(refreshToken string, ipAddress string, deviceInfo string) (*TokenPair, error)
}

type SendEmailVerificationUseCase interface {
	Execute(email string) error
}

type VerifyEmailTokenUseCase interface {
	Execute(token string) error
}

type ChangePasswordUseCase interface {
	Execute(input ChangePasswordInput) error
}

type LogoutUseCase interface {
	Execute(userID string, sessionID string, accessJTI string) error
}

type LogoutAllUseCase interface {
	Execute(userID string) error
}

type ListSessionsUseCase interface {
	Execute(userID string) ([]*Session, error)
}

type RevokeSessionUseCase interface {
	Execute(userID string, sessionID string) error
}
