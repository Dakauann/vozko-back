package container

import (
	"context"
	"testing"

	"vozko/domain/shared"
	stage_domain "vozko/domain/stage"
)

type recordingRegistrar struct {
	registered map[shared.EntryType]stage_domain.ContainerPipelineResolver
}

func (r *recordingRegistrar) SetContainerPipelineResolver(
	entryType shared.EntryType,
	resolver stage_domain.ContainerPipelineResolver,
) {
	if r.registered == nil {
		r.registered = map[shared.EntryType]stage_domain.ContainerPipelineResolver{}
	}
	r.registered[entryType] = resolver
}

func TestEveryConversationChannelRegistersAPipelineResolver(t *testing.T) {
	registrar := &recordingRegistrar{}
	for entryType, resolver := range containerPipelineResolvers(nil) {
		registrar.SetContainerPipelineResolver(entryType, resolver)
	}

	for _, entryType := range []shared.EntryType{
		shared.EntryTypeWhatsApp,
		shared.EntryTypeInstagram,
		shared.EntryTypeTelegram,
		shared.EntryTypeUnofficialWhatsApp,
	} {
		resolver, found := registrar.registered[entryType]
		if !found {
			t.Errorf("channel %q has no pipeline resolver; its conversations would "+
				"silently fall back to the workspace default funnel", entryType)
			continue
		}
		if resolver == nil {
			t.Errorf("channel %q registered a nil resolver", entryType)
		}
	}
}

type staticResolver struct {
	pipelineID string
	err        error
	asked      []string
}

func (r *staticResolver) PipelineIDForContainer(_ context.Context, containerID string) (string, error) {
	r.asked = append(r.asked, containerID)
	return r.pipelineID, r.err
}

func TestResolversAreKeyedByTheirOwnChannel(t *testing.T) {
	resolvers := containerPipelineResolvers(nil)

	if len(resolvers) != 4 {
		t.Fatalf("registered %d resolvers, want one per conversation channel", len(resolvers))
	}
	for entryType, resolver := range resolvers {
		if resolver == nil {
			t.Errorf("channel %q maps to a nil resolver", entryType)
		}
		if !entryType.Valid() {
			t.Errorf("%q is not a known entry type", entryType)
		}
	}
}
