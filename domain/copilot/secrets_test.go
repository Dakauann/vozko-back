package copilot

import (
	"errors"
	"testing"
)

var password = []SecretField{{Key: "password", Label: "Senha da linha"}}

func TestTheModelNeverCarriesASecret(t *testing.T) {
	args := map[string]interface{}{"name": "Principal", "password": "leaked-in-chat"}
	stripped := StripSecrets(args, password)
	if _, present := stripped["password"]; present {
		t.Fatal("a secret the model sent survived into the proposal")
	}
	if stripped["name"] != "Principal" || args["password"] != "leaked-in-chat" {
		t.Fatal("stripping must copy, not mutate the model's arguments")
	}
}

func TestSecretsJoinTheArgumentsOnlyAtApproval(t *testing.T) {
	args := map[string]interface{}{"name": "Principal"}
	merged, err := WithSecrets(args, password, map[string]string{"password": "s3nh4 forte", "host": "evil"})
	if err != nil {
		t.Fatal(err)
	}
	if merged["password"] != "s3nh4 forte" || merged["name"] != "Principal" {
		t.Fatalf("merged = %v", merged)
	}
	if _, injected := merged["host"]; injected {
		t.Fatal("only declared secrets may be supplied at approval")
	}
	if _, leaked := args["password"]; leaked {
		t.Fatal("the stored arguments gained the secret")
	}
}

func TestAMissingSecretStopsTheChange(t *testing.T) {
	if _, err := WithSecrets(map[string]interface{}{}, password, map[string]string{"password": ""}); !errors.Is(err, ErrSecretMissing) {
		t.Fatalf("err = %v, want ErrSecretMissing", err)
	}
	if _, err := WithSecrets(map[string]interface{}{}, password, nil); !errors.Is(err, ErrSecretMissing) {
		t.Fatalf("err = %v, want ErrSecretMissing", err)
	}
}
