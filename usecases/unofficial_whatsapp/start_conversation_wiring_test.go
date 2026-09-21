package unofficial_whatsapp

import "testing"

func TestNewStartConversationUseCaseRefusesNilDependencies(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("a use case with no instance repository was constructed successfully")
		}
	}()
	_ = NewStartConversationUseCase(nil, nil, nil, nil, nil, nil)
}
