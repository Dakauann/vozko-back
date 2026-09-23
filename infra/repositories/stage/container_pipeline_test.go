package stage_repository

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/shared"
	"vozko/domain/stage"
)

type fakeResolver struct {
	pipelineID string
	err        error
	asked      []string
}

func (r *fakeResolver) PipelineIDForContainer(_ context.Context, containerID string) (string, error) {
	r.asked = append(r.asked, containerID)
	return r.pipelineID, r.err
}

func repoWith(resolvers map[shared.EntryType]stage.ContainerPipelineResolver) *repository {
	return &repository{pipelineResolvers: resolvers}
}

func TestEachChannelIsAskedOnlyItsOwnResolver(t *testing.T) {
	instagram := &fakeResolver{pipelineID: "pipe-instagram"}
	telegram := &fakeResolver{pipelineID: "pipe-telegram"}

	repo := repoWith(map[shared.EntryType]stage.ContainerPipelineResolver{
		shared.EntryTypeInstagram: instagram,
		shared.EntryTypeTelegram:  telegram,
	})

	if got := repo.campaignPipelineID("acc-1", string(shared.EntryTypeInstagram)); got != "pipe-instagram" {
		t.Fatalf("instagram resolved %q", got)
	}
	if got := repo.campaignPipelineID("acc-2", string(shared.EntryTypeTelegram)); got != "pipe-telegram" {
		t.Fatalf("telegram resolved %q", got)
	}

	if len(instagram.asked) != 1 || instagram.asked[0] != "acc-1" {
		t.Fatalf("instagram was asked %v", instagram.asked)
	}
	if len(telegram.asked) != 1 || telegram.asked[0] != "acc-2" {
		t.Fatalf("telegram was asked %v; a channel must never answer for another", telegram.asked)
	}
}

func TestAnUnregisteredChannelFallsBackInsteadOfGuessing(t *testing.T) {
	instagram := &fakeResolver{pipelineID: "pipe-instagram"}
	repo := repoWith(map[shared.EntryType]stage.ContainerPipelineResolver{
		shared.EntryTypeInstagram: instagram,
	})

	if got := repo.campaignPipelineID("acc-9", string(shared.EntryTypeTelegram)); got != "" {
		t.Fatalf("telegram resolved %q with no resolver registered", got)
	}
	if len(instagram.asked) != 0 {
		t.Fatalf("instagram was asked about a telegram container: %v", instagram.asked)
	}
}

func TestAResolverFailureFallsBackToTheDefaultFunnel(t *testing.T) {
	repo := repoWith(map[shared.EntryType]stage.ContainerPipelineResolver{
		shared.EntryTypeInstagram: &fakeResolver{err: errors.New("account table unreachable")},
	})

	if got := repo.campaignPipelineID("acc-1", string(shared.EntryTypeInstagram)); got != "" {
		t.Fatalf("a failed lookup returned %q; it must fall back, never guess a funnel", got)
	}
}

func TestAContainerWithNoFunnelFallsBack(t *testing.T) {
	repo := repoWith(map[shared.EntryType]stage.ContainerPipelineResolver{
		shared.EntryTypeInstagram: &fakeResolver{pipelineID: "   "},
	})

	if got := repo.campaignPipelineID("acc-1", string(shared.EntryTypeInstagram)); got != "" {
		t.Fatalf("blank pipeline resolved to %q", got)
	}
}

func TestTheStageRepositoryNamesNoChannelTable(t *testing.T) {
	repo := repoWith(nil)

	for _, entryType := range []shared.EntryType{
		shared.EntryTypeWhatsApp,
		shared.EntryTypeInstagram,
		shared.EntryTypeTelegram,
		shared.EntryTypeUnofficialWhatsApp,
	} {
		if got := repo.campaignPipelineID("any", string(entryType)); got != "" {
			t.Fatalf("%q resolved %q with no resolvers at all; the repository still "+
				"knows a channel's storage", entryType, got)
		}
	}
}
