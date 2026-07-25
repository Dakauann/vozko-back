package auth

type TokenPair struct {
	AccessToken  string
	RefreshToken string
	AccessJTI    string
	UserID       string
	Email        string
	Name         string
	Picture      string
	Role         string
	CustomerType string
}

type CredentialsInput struct {
	Name              string
	Email             string
	Password          string
	CustomerType      string
	CPF               string
	CNPJ              string
	VerificationToken string
	DeviceInfo        string
	IPAddress         string

	ReferralCode string
}

type ChangePasswordInput struct {
	UserID          string
	CurrentPassword string
	NewPassword     string
}
