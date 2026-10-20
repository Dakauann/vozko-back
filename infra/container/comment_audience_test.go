package container

import (
	"context"
	"testing"

	ca "vozko/domain/audience"
	fbdomain "vozko/domain/facebook"
	igdomain "vozko/domain/instagram"
	iguc "vozko/usecases/instagram"
)

type instagramOnlyAdapter struct{ ca.SourceAdapter }

func (instagramOnlyAdapter) Enqueue(context.Context, *igdomain.Comment) {}
func (instagramOnlyAdapter) Forget(context.Context, string)             {}

type facebookOnlyAdapter struct{ ca.SourceAdapter }

func (facebookOnlyAdapter) Enqueue(context.Context, *fbdomain.Comment) {}
func (facebookOnlyAdapter) Forget(context.Context, string)             {}

func TestEachCommentSourceGetsItsOwnAudienceEnqueuer(t *testing.T) {
	bundle := &audienceBundle{Enabled: true, CommentAdapters: map[ca.Source]ca.SourceAdapter{
		ca.SourceInstagram: instagramOnlyAdapter{},
		ca.SourceFacebook:  facebookOnlyAdapter{},
	}}

	ig, ok := commentAudience[iguc.AudienceEnqueuer](bundle, ca.SourceInstagram)
	if _, right := ig.(instagramOnlyAdapter); !ok || !right {
		t.Fatalf("instagram enqueuer = %T, %v", ig, ok)
	}
	fb, ok := commentAudience[facebookAudience](bundle, ca.SourceFacebook)
	if _, right := fb.(facebookOnlyAdapter); !ok || !right {
		t.Fatalf("facebook enqueuer = %T, %v", fb, ok)
	}
}

func TestASourceWithoutAnAdapterGetsNoEnqueuer(t *testing.T) {
	instagramOnly := &audienceBundle{Enabled: true, CommentAdapters: map[ca.Source]ca.SourceAdapter{
		ca.SourceInstagram: instagramOnlyAdapter{},
	}}
	if got, ok := commentAudience[facebookAudience](instagramOnly, ca.SourceFacebook); ok || got != nil {
		t.Fatalf("facebook must not borrow another channel's adapter: %T", got)
	}
	if got, ok := commentAudience[iguc.AudienceEnqueuer](nil, ca.SourceInstagram); ok || got != nil {
		t.Fatalf("no bundle, no enqueuer: %T", got)
	}
	disabled := &audienceBundle{CommentAdapters: instagramOnly.CommentAdapters}
	if got, ok := commentAudience[iguc.AudienceEnqueuer](disabled, ca.SourceInstagram); ok || got != nil {
		t.Fatalf("a disabled bundle hands out nothing: %T", got)
	}
}

func TestAnAdapterThatCannotEnqueueForItsSourceFailsWiring(t *testing.T) {
	crossed := &audienceBundle{Enabled: true, CommentAdapters: map[ca.Source]ca.SourceAdapter{
		ca.SourceFacebook: instagramOnlyAdapter{},
	}}
	defer func() {
		if recover() == nil {
			t.Fatal("a source registered with the wrong adapter must fail at wiring time")
		}
	}()
	commentAudience[facebookAudience](crossed, ca.SourceFacebook)
}
