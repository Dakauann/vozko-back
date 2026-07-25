package security

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"vozko/domain/auth"
	"vozko/domain/user"
)

func TestNewJWTTokenService_ValidParams(t *testing.T) {
	svc, err := NewJWTTokenService("secret", 15*time.Minute, 24*time.Hour)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if svc == nil {
		t.Fatal("expected non-nil service")
	}
}

func TestNewJWTTokenService_EmptySecret(t *testing.T) {
	_, err := NewJWTTokenService("", 15*time.Minute, 24*time.Hour)
	if err == nil {
		t.Fatal("expected error for empty secret")
	}
}

func TestNewJWTTokenService_ZeroAccessTTL(t *testing.T) {
	_, err := NewJWTTokenService("secret", 0, 24*time.Hour)
	if err == nil {
		t.Fatal("expected error for zero access TTL")
	}
}

func TestNewJWTTokenService_NegativeRefreshTTL(t *testing.T) {
	_, err := NewJWTTokenService("secret", 15*time.Minute, -1*time.Hour)
	if err == nil {
		t.Fatal("expected error for negative refresh TTL")
	}
}

func TestNewJWTTokenService_ZeroRefreshTTL(t *testing.T) {
	_, err := NewJWTTokenService("secret", 15*time.Minute, 0)
	if err == nil {
		t.Fatal("expected error for zero refresh TTL")
	}
}

func newTestService(t *testing.T) *JWTTokenService {
	t.Helper()
	svc, err := NewJWTTokenService("test-secret-key-for-unit-tests", 15*time.Minute, 720*time.Hour)
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}
	return svc
}

func TestIssue_ValidUser(t *testing.T) {
	svc := newTestService(t)
	u := &user.User{
		ID:           "user-123",
		Email:        "test@example.com",
		Username:     "Test User",
		Role:         user.RoleUser,
		CustomerType: user.CustomerTypeIndividual,
	}

	pair, err := svc.Issue(u)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if pair.AccessToken == "" {
		t.Error("expected non-empty access token")
	}
	if pair.AccessJTI == "" {
		t.Error("expected non-empty AccessJTI")
	}
	if pair.UserID != "user-123" {
		t.Errorf("expected userID user-123, got %s", pair.UserID)
	}
	if pair.Email != "test@example.com" {
		t.Errorf("expected email test@example.com, got %s", pair.Email)
	}
	if pair.Name != "Test User" {
		t.Errorf("expected name Test User, got %s", pair.Name)
	}
	if pair.Role != "user" {
		t.Errorf("expected role user, got %s", pair.Role)
	}
	if pair.CustomerType != "individual" {
		t.Errorf("expected customerType individual, got %s", pair.CustomerType)
	}
}

func TestIssue_AdminRole(t *testing.T) {
	svc := newTestService(t)
	u := &user.User{
		ID:    "admin-1",
		Email: "admin@example.com",
		Role:  user.RoleAdmin,
	}

	pair, err := svc.Issue(u)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if pair.Role != "admin" {
		t.Errorf("expected role admin, got %s", pair.Role)
	}
}

func TestIssue_EmptyRole_DefaultsToUser(t *testing.T) {
	svc := newTestService(t)
	u := &user.User{
		ID:    "user-no-role",
		Email: "norole@example.com",
		Role:  "",
	}

	pair, err := svc.Issue(u)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if pair.Role != "user" {
		t.Errorf("expected default role user, got %s", pair.Role)
	}
}

func TestIssue_NilUser(t *testing.T) {
	svc := newTestService(t)
	_, err := svc.Issue(nil)
	if err == nil {
		t.Fatal("expected error for nil user")
	}
}

func TestIssue_EmptyUserID(t *testing.T) {
	svc := newTestService(t)
	u := &user.User{ID: "", Email: "e@e.com"}
	_, err := svc.Issue(u)
	if err == nil {
		t.Fatal("expected error for empty user ID")
	}
}

func TestIssue_DisabledUser(t *testing.T) {
	svc := newTestService(t)
	disabledAt := time.Now()
	u := &user.User{ID: "disabled-user", Email: "disabled@example.com", DisabledAt: &disabledAt}

	if _, err := svc.Issue(u); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("expected invalid credentials for disabled user, got %v", err)
	}
}

func TestIssue_TokensAreDifferent(t *testing.T) {
	svc := newTestService(t)
	u := &user.User{ID: "user-1", Email: "a@b.com", Role: user.RoleUser}
	pair, err := svc.Issue(u)
	if err != nil {
		t.Fatal(err)
	}
	if pair.AccessToken == pair.RefreshToken {
		t.Error("access and refresh tokens must not be identical")
	}
}

func TestVerify_ValidAccessToken(t *testing.T) {
	svc := newTestService(t)
	u := &user.User{
		ID:           "user-1",
		Email:        "verify@test.com",
		Role:         user.RoleUser,
		CustomerType: user.CustomerTypeCompany,
	}

	pair, _ := svc.Issue(u)
	claims, err := svc.Verify(pair.AccessToken)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if claims.UserID != "user-1" {
		t.Errorf("expected userID user-1, got %s", claims.UserID)
	}
	if claims.Email != "verify@test.com" {
		t.Errorf("expected email verify@test.com, got %s", claims.Email)
	}
	if claims.Role != "user" {
		t.Errorf("expected role user, got %s", claims.Role)
	}
	if claims.Customer != "company" {
		t.Errorf("expected customer company, got %s", claims.Customer)
	}
}

func TestVerify_RefreshTokenRejected(t *testing.T) {
	svc := newTestService(t)
	u := &user.User{ID: "user-1", Email: "r@t.com", Role: user.RoleUser}
	pair, _ := svc.Issue(u)

	_, err := svc.Verify(pair.RefreshToken)
	if err == nil {
		t.Fatal("expected error when verifying refresh token as access")
	}
}

func TestVerify_ExpiredToken(t *testing.T) {
	svc, _ := NewJWTTokenService("test-secret", 1*time.Nanosecond, 720*time.Hour)
	u := &user.User{ID: "user-1", Email: "e@e.com", Role: user.RoleUser}
	pair, _ := svc.Issue(u)

	time.Sleep(5 * time.Millisecond)

	_, err := svc.Verify(pair.AccessToken)
	if err == nil {
		t.Fatal("expected error for expired token")
	}
}

func TestVerify_InvalidTokenString(t *testing.T) {
	svc := newTestService(t)
	_, err := svc.Verify("not.a.valid.token")
	if err == nil {
		t.Fatal("expected error for invalid token")
	}
}

func TestVerify_EmptyToken(t *testing.T) {
	svc := newTestService(t)
	_, err := svc.Verify("")
	if err == nil {
		t.Fatal("expected error for empty token")
	}
}

func TestVerify_WrongSigningMethod(t *testing.T) {

	svc := newTestService(t)

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":          "user-1",
		"email":        "test@test.com",
		"role":         "user",
		"customerType": "individual",
		"typ":          "access",
		"iat":          time.Now().Unix(),
		"exp":          time.Now().Add(15 * time.Minute).Unix(),
	})

	token.Header["alg"] = "RS256"

	tokenStr, _ := token.SignedString(svc.secret)
	_, err := svc.Verify(tokenStr)
	if err == nil {
		t.Fatal("expected error for wrong signing method")
	}
}

func TestVerify_TokenWithNoSubClaim(t *testing.T) {
	svc := newTestService(t)

	claims := jwt.MapClaims{
		"email": "test@test.com",
		"role":  "user",
		"typ":   "access",
		"iat":   time.Now().Unix(),
		"exp":   time.Now().Add(15 * time.Minute).Unix(),
	}

	token, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(svc.secret)
	_, err := svc.Verify(token)
	if err == nil {
		t.Fatal("expected error for missing sub claim")
	}
}

func TestVerify_TokenWithWrongSecret(t *testing.T) {
	svc := newTestService(t)
	otherSvc, _ := NewJWTTokenService("different-secret", 15*time.Minute, 720*time.Hour)

	u := &user.User{ID: "user-1", Email: "e@e.com", Role: user.RoleUser}
	pair, _ := otherSvc.Issue(u)

	_, err := svc.Verify(pair.AccessToken)
	if err == nil {
		t.Fatal("expected error for wrong secret")
	}
}

func TestVerify_TokenWithoutTypClaim(t *testing.T) {
	svc := newTestService(t)

	claims := jwt.MapClaims{
		"sub":   "user-1",
		"email": "test@test.com",
		"role":  "user",
		"iat":   time.Now().Unix(),
		"exp":   time.Now().Add(15 * time.Minute).Unix(),
	}

	token, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(svc.secret)
	_, err := svc.Verify(token)
	if err == nil {
		t.Fatal("expected error for missing typ claim")
	}
}

func TestGenerateRefreshToken_ReturnsRawAndHash(t *testing.T) {
	svc := newTestService(t)
	raw, hash, err := svc.GenerateRefreshToken()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if raw == "" {
		t.Fatal("expected non-empty raw token")
	}
	if hash == "" {
		t.Fatal("expected non-empty hash")
	}
	if raw == hash {
		t.Fatal("raw and hash should differ")
	}

	if len(raw) != 64 {
		t.Errorf("expected raw token length 64, got %d", len(raw))
	}
}

func TestGenerateRefreshToken_UniqueEachCall(t *testing.T) {
	svc := newTestService(t)
	raw1, _, _ := svc.GenerateRefreshToken()
	raw2, _, _ := svc.GenerateRefreshToken()
	if raw1 == raw2 {
		t.Fatal("expected unique tokens on separate calls")
	}
}

func TestHashRefreshToken_Deterministic(t *testing.T) {
	svc := newTestService(t)
	h1 := svc.HashRefreshToken("test-token")
	h2 := svc.HashRefreshToken("test-token")
	if h1 != h2 {
		t.Fatal("expected identical hash for same input")
	}
}

func TestHashRefreshToken_DifferentInputsDifferentHashes(t *testing.T) {
	svc := newTestService(t)
	h1 := svc.HashRefreshToken("token-a")
	h2 := svc.HashRefreshToken("token-b")
	if h1 == h2 {
		t.Fatal("expected different hashes for different inputs")
	}
}

func TestGenerateRefreshToken_HashMatchesRaw(t *testing.T) {
	svc := newTestService(t)
	raw, hash, err := svc.GenerateRefreshToken()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	reHash := svc.HashRefreshToken(raw)
	if hash != reHash {
		t.Errorf("HashRefreshToken(raw) = %s, want %s", reHash, hash)
	}
}

func TestIssue_AccessTokenContainsExpectedClaims(t *testing.T) {
	svc := newTestService(t)
	u := &user.User{
		ID:           "user-claims-check",
		Email:        "claims@test.com",
		Role:         user.RoleAdmin,
		CustomerType: user.CustomerTypeCompany,
	}

	pair, _ := svc.Issue(u)

	parser := jwt.NewParser(jwt.WithoutClaimsValidation())
	token, _, err := parser.ParseUnverified(pair.AccessToken, jwt.MapClaims{})
	if err != nil {
		t.Fatalf("failed to parse token: %v", err)
	}

	claims := token.Claims.(jwt.MapClaims)
	if claims["typ"] != "access" {
		t.Errorf("expected typ=access, got %v", claims["typ"])
	}
	if claims["sub"] != "user-claims-check" {
		t.Errorf("expected sub=user-claims-check, got %v", claims["sub"])
	}
	if claims["email"] != "claims@test.com" {
		t.Errorf("expected email claims@test.com, got %v", claims["email"])
	}
	if claims["role"] != "admin" {
		t.Errorf("expected role admin, got %v", claims["role"])
	}
	if claims["customerType"] != "company" {
		t.Errorf("expected customerType company, got %v", claims["customerType"])
	}
}

func TestIssue_AccessTokenContainsJTIAndVersionClaims(t *testing.T) {
	svc := newTestService(t)
	u := &user.User{ID: "user-rt-check", Email: "rt@test.com", Role: user.RoleUser, TokenVersion: 3}
	pair, _ := svc.Issue(u)

	if pair.AccessJTI == "" {
		t.Fatal("expected non-empty AccessJTI in token pair")
	}

	if pair.RefreshToken != "" {
		t.Errorf("expected empty RefreshToken from Issue, got %s", pair.RefreshToken)
	}

	parser := jwt.NewParser(jwt.WithoutClaimsValidation())
	token, _, err := parser.ParseUnverified(pair.AccessToken, jwt.MapClaims{})
	if err != nil {
		t.Fatalf("failed to parse access token: %v", err)
	}

	claims := token.Claims.(jwt.MapClaims)
	if claims["jti"] == nil || claims["jti"] == "" {
		t.Error("expected jti claim in access token")
	}
	if ver, ok := claims["ver"].(float64); !ok || int(ver) != 3 {
		t.Errorf("expected ver=3 in access token, got %v", claims["ver"])
	}
}

func TestVerify_DoesNotAcceptNoneAlgorithm(t *testing.T) {
	svc := newTestService(t)

	claims := jwt.MapClaims{
		"sub":          "hacker",
		"email":        "hacker@evil.com",
		"role":         "admin",
		"customerType": "individual",
		"typ":          "access",
		"iat":          time.Now().Unix(),
		"exp":          time.Now().Add(15 * time.Minute).Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodNone, claims)
	tokenStr, _ := token.SignedString(jwt.UnsafeAllowNoneSignatureType)

	_, err := svc.Verify(tokenStr)
	if err == nil {
		t.Fatal("SECURITY: 'none' algorithm attack should be rejected")
	}
}

func TestVerify_RejectsTokenWithManipulatedClaims(t *testing.T) {
	svc := newTestService(t)
	u := &user.User{ID: "user-1", Email: "legit@test.com", Role: user.RoleUser}
	pair, _ := svc.Issue(u)

	parts := strings.Split(pair.AccessToken, ".")
	if len(parts) != 3 {
		t.Fatal("expected 3 JWT parts")
	}

	parts[1] = parts[1] + "x"
	tampered := strings.Join(parts, ".")

	_, err := svc.Verify(tampered)
	if err == nil {
		t.Fatal("SECURITY: tampered token should be rejected")
	}
}
