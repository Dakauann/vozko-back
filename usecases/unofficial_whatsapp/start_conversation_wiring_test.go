package unofficial_whatsapp

import "testing"

// A use case built from nil dependencies is a use case that panics on its first
// real call, somewhere far from the mistake.
//
// This happened: the composition root constructed this from bundle fields it
// had not assigned yet, so four repositories were nil, and the only caller (an
// alert) panicked mid-request. Refusing at construction turns that into a boot
// failure nobody can deploy past.
func TestNewStartConversationUseCaseRefusesNilDependencies(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("a use case with no instance repository was constructed successfully")
		}
	}()
	_ = NewStartConversationUseCase(nil, nil, nil, nil, nil, nil)
}
