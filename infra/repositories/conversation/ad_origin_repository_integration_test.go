package conversation_repository

import (
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func TestOnlyTheFirstAdOfAConversationIsKeptEvenUnderRaces(t *testing.T) {
	repo := NewAdOriginRepository(repotest.IsolatedDB(t, "adorigin_test", &schema.ConversationAdOrigin{}))
	entry := uuid.NewString()
	image := uuid.NewString()

	var wg sync.WaitGroup
	var mu sync.Mutex
	wins := 0
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			title := "Primeiro"
			if i > 0 {
				title = "Concorrente"
			}
			claimed, err := repo.Claim(&conversation.AdOrigin{
				EntryID: entry, EntryType: shared.EntryTypeWhatsApp, AdID: "ad-1", Title: title,
				Platform: conversation.AdPlatformInstagram, ImageMediaID: image, ArrivedAt: time.Now(),
			})
			if err != nil {
				t.Error(err)
			}
			if claimed {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("wins = %d, want exactly one", wins)
	}

	origin, err := repo.Get(entry, shared.EntryTypeWhatsApp)
	if err != nil || origin == nil || origin.ImageMediaID != image || origin.Platform != conversation.AdPlatformInstagram {
		t.Fatalf("origin=%+v err=%v", origin, err)
	}
	if other, err := repo.Get(entry, shared.EntryTypeInstagram); err != nil || other != nil {
		t.Fatalf("the same id on another channel is another conversation: %+v %v", other, err)
	}
}
