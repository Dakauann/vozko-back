package tools_usecase

import (
	"strings"
	"testing"
)

func VerifyDefinitionTest(t *testing.T) {
	generatePaymentToolUseCase := NewGeneratePaymentToolUseCase()
	prefix := "Gera um link"
	if !strings.HasPrefix(generatePaymentToolUseCase.Definition().Description, prefix) {
		t.Fatalf("Tool name doesnt satisfy naming patterns for description, expected %s", prefix)
	}
}
