package workflow

import (
	"reflect"
	"testing"
)

func stateWithSecretLookalike() *RunState {
	s := NewRunState()
	s.Set("secret", map[string]interface{}{"api_key": "from-state"})
	s.Set("nome", "Ana")
	s.Set("message", "{{secret.api_key}}")
	return &s
}

func TestInterpolateNeverResolvesTheSecretScopeFromRunState(t *testing.T) {
	got := Interpolate("k={{secret.api_key}} n={{nome}}", stateWithSecretLookalike(), nil)
	if got != "k={{secret.api_key}} n=Ana" {
		t.Fatalf("got %q", got)
	}
	if _, ok := ResolveVariable("secret.api_key", stateWithSecretLookalike()); ok {
		t.Fatal("the secret scope must never resolve from state")
	}
}

func TestInterpolateWithSecretsResolvesSecretsAndStateInOnePass(t *testing.T) {
	secrets := map[string]string{"api_key": "real"}
	got := InterpolateWithSecrets("k={{ secret.api_key }} n={{nome}}", stateWithSecretLookalike(), nil, secrets)
	if got != "k=real n=Ana" {
		t.Fatalf("got %q", got)
	}
}

func TestASecretPlaceholderTypedIntoStateIsNeverResolved(t *testing.T) {
	secrets := map[string]string{"api_key": "real"}
	got := InterpolateWithSecrets("msg={{message}}", stateWithSecretLookalike(), nil, secrets)
	if got != "msg={{secret.api_key}}" {
		t.Fatalf("a lead-typed placeholder leaked the secret: %q", got)
	}
}

func TestSecretRefsCollectsEveryReferencedNameOnceInOrder(t *testing.T) {
	config := map[string]interface{}{
		"url":     "https://api.test/{{secret.tenant}}/x?k={{secret.api_key}}",
		"headers": map[string]interface{}{"Authorization": "Bearer {{secret.api_key}}"},
		"list":    []interface{}{"{{secret.zeta}}", 3},
		"body":    "{{nome}}",
	}
	got := SecretRefs(config)
	if !reflect.DeepEqual(got, []string{"api_key", "tenant", "zeta"}) {
		t.Fatalf("got %v", got)
	}
	if got := SecretRefs(map[string]interface{}{"url": "https://plain"}); len(got) != 0 {
		t.Fatalf("got %v", got)
	}
}

func TestCodeSecretNamesReadsOnlyLiteralSecretsGetCalls(t *testing.T) {
	code := `const a = secrets.get('unifecaf_login');
const b = secrets.get( "unifecaf_senha" );
const c = secrets.get(dynamicName);
const d = secrets.get('unifecaf_login');`
	got := CodeSecretNames(code)
	if !reflect.DeepEqual(got, []string{"unifecaf_login", "unifecaf_senha"}) {
		t.Fatalf("got %v", got)
	}
}

func TestSecretReferencesNeedNoMockForANodeTest(t *testing.T) {
	deps := ExtractDependencies(map[string]interface{}{"url": "https://x/{{secret.api_key}}"})
	if RequiresUpstreamData(deps) {
		t.Fatal("a secret is resolved by the server and needs no upstream data")
	}
	if mocks := GetRequiredMocks(deps); len(mocks) != 0 {
		t.Fatalf("a secret must not be asked as a mock: %+v", mocks)
	}
}
