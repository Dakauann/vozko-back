package security

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"vozko/domain/auth"
	"vozko/domain/user"
)

type JWTTokenService struct {
	secret     []byte
	accessTTL  time.Duration
	refreshTTL time.Duration
}

func NewJWTTokenService(secret string, accessTTL, refreshTTL time.Duration) (*JWTTokenService, error) {
	if secret == "" {
		return nil, errors.New("jwt secret required")
	}

	if accessTTL <= 0 || refreshTTL <= 0 {
		return nil, errors.New("token ttl required")
	}

	return &JWTTokenService{
		secret:     []byte(secret),
		accessTTL:  accessTTL,
		refreshTTL: refreshTTL,
	}, nil
}

func (s *JWTTokenService) Issue(u *user.User) (*auth.TokenPair, error) {
	if u == nil || u.ID == "" {
		return nil, errors.New("invalid user payload")
	}
	if u.DisabledAt != nil {
		return nil, auth.ErrInvalidCredentials
	}

	role := string(u.Role)
	if role == "" {
		role = string(user.RoleUser)
	}

	now := time.Now().UTC()
	jti := uuid.New().String()

	accessClaims := jwt.MapClaims{
		"sub":          u.ID,
		"email":        u.Email,
		"role":         role,
		"customerType": string(u.CustomerType),
		"typ":          "access",
		"ver":          u.TokenVersion,
		"jti":          jti,
		"iat":          now.Unix(),
		"exp":          now.Add(s.accessTTL).Unix(),
	}

	accessToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims).SignedString(s.secret)
	if err != nil {
		return nil, err
	}

	return &auth.TokenPair{
		AccessToken:  accessToken,
		RefreshToken: "",
		AccessJTI:    jti,
		UserID:       u.ID,
		Email:        u.Email,
		Name:         u.Username,
		Picture:      u.Picture,
		Role:         role,
		CustomerType: string(u.CustomerType),
	}, nil
}

func (s *JWTTokenService) Verify(tokenString string) (*auth.Claims, error) {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("invalid signing method")
		}
		return s.secret, nil
	})

	if err != nil {
		return nil, err
	}

	if !token.Valid {
		return nil, errors.New("invalid token")
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, errors.New("invalid token claims")
	}

	typ, _ := claims["typ"].(string)
	if typ != "access" {
		return nil, errors.New("invalid token type")
	}

	userID, _ := claims["sub"].(string)
	email, _ := claims["email"].(string)
	role, _ := claims["role"].(string)
	customer, _ := claims["customerType"].(string)
	jtiVal, _ := claims["jti"].(string)
	verVal, _ := claims["ver"].(float64)

	if userID == "" {
		return nil, errors.New("invalid token claims")
	}

	return &auth.Claims{
		UserID:       userID,
		Email:        email,
		Role:         role,
		Customer:     customer,
		TokenVersion: int(verVal),
		JTI:          jtiVal,
	}, nil
}

func (s *JWTTokenService) GenerateRefreshToken() (raw string, hash string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", fmt.Errorf("generate refresh token: %w", err)
	}
	raw = hex.EncodeToString(b)
	hash = s.HashRefreshToken(raw)
	return raw, hash, nil
}

func (s *JWTTokenService) HashRefreshToken(raw string) string {
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:])
}
