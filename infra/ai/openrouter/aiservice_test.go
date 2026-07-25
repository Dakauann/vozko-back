package openrouter

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"vozko/domain/ai"

	"github.com/joho/godotenv"
)

func TestAiCompletetion(t *testing.T) {

	envPath := filepath.Join("..", "..", "..", ".env")
	_ = godotenv.Load(envPath)

	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		t.Skip("OPENROUTER_API_KEY not set, skipping test")
	}

	aiService := NewService(Config{
		APIKey:       apiKey,
		DefaultModel: "x-ai/grok-4.1-fast",
	}, nil, nil)

	ctx := context.Background()
	response, err := aiService.Generate(ctx, ai.GenerateInput{
		Model: "x-ai/grok-4.1-fast",
		SystemPrompt: `Você responde como uma pessoa real conversando no WhatsApp.
REGRA CRÍTICA: Divida sua resposta em várias mensagens curtas (5-15 palavras cada).
NUNCA escreva uma mensagem longa. Cada item do array é uma mensagem separada.
Exemplo correto: ["Oi!", "Sou seu professor de IA", "Prazer em te conhecer"]
Exemplo ERRADO: ["Oi, sou seu professor de IA e é um prazer te conhecer..."]`,
		Messages: []ai.Message{
			{Role: ai.RoleUser, Content: "Se apresente para mim, como se fosse meu professor de IA, fale uma frase longa. seja sincero"},
		},
		Temperature:       0.1,
		MaxTokens:         256,
		ToolExecutionMode: ai.ToolExecutionModeNone,
	})

	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	t.Logf("Raw content: %s", response.Message.Content)
	t.Logf("Parsed messages (%d):", len(response.Messages))
	for i, msg := range response.Messages {
		t.Logf("  [%d]: %s", i, msg)
	}

}
