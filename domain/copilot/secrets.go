package copilot

import (
	"errors"
	"fmt"
)

var ErrSecretMissing = errors.New("a secret field was not filled in")

type SecretField struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

type SecretAsker interface {
	Secrets(args map[string]interface{}) []SecretField
}

type SecretConcealer interface {
	Conceal(args map[string]interface{}) map[string]interface{}
}

func Conceal(tool Tool, args map[string]interface{}, secrets []SecretField) map[string]interface{} {
	stripped := StripSecrets(args, secrets)
	if concealer, ok := tool.(SecretConcealer); ok {
		return concealer.Conceal(stripped)
	}
	return stripped
}

func StripSecrets(args map[string]interface{}, secrets []SecretField) map[string]interface{} {
	out := make(map[string]interface{}, len(args))
	for key, value := range args {
		out[key] = value
	}
	for _, secret := range secrets {
		delete(out, secret.Key)
	}
	return out
}

func WithSecrets(args map[string]interface{}, secrets []SecretField, provided map[string]string) (map[string]interface{}, error) {
	out := StripSecrets(args, secrets)
	for _, secret := range secrets {
		value := provided[secret.Key]
		if value == "" {
			return nil, fmt.Errorf("%w: %s", ErrSecretMissing, secret.Label)
		}
		out[secret.Key] = value
	}
	return out, nil
}
