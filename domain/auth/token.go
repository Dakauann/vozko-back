package auth

type TokenVerifier interface {
	Verify(token string) (*Claims, error)
}

type Claims struct {
	UserID       string
	Email        string
	Role         string
	Customer     string
	TokenVersion int
	JTI          string
}
