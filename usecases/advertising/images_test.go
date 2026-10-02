package advertising

import (
	"context"
	"errors"
	"testing"

	ads "vozko/domain/advertising"
	"vozko/domain/media"
)

type fakeGenerator struct {
	calls int
	cost  int64
}

func (g *fakeGenerator) Generate(context.Context, ads.ImageRequest) (*ads.GeneratedImage, error) {
	g.calls++
	return &ads.GeneratedImage{Bytes: []byte("jpg"), MIMEType: "image/jpeg", Model: "openai/gpt-image-2.5-flare", ProviderCostMicros: g.cost}, nil
}

type fakeFunds struct{ err error }

func (f fakeFunds) Check(string) error { return f.err }

type billedEvent struct {
	model string
	cost  int64
}

type fakeAIBilling struct{ events []billedEvent }

func (b *fakeAIBilling) Publish(_, model string, _, _ int, cost int64) {
	b.events = append(b.events, billedEvent{model: model, cost: cost})
}

type fakeUploader struct{ names []string }

func (u *fakeUploader) UploadMedia(ws string, _ []byte, name string, kind media.MediaType, _ string) (media.Media, error) {
	u.names = append(u.names, name)
	return media.Media{ID: "m-1", WorkspaceID: ws, URL: "https://cdn/" + name, Type: kind}, nil
}

func imageRequest() ads.ImageRequest {
	return ads.ImageRequest{WorkspaceID: "ws-1", Prompt: "pizza artesanal", Aspect: ads.AspectSquare}
}

func TestGeneratedImageIsBilledAsAIAndStoredInTheMediaLibrary(t *testing.T) {
	gen, bill, up := &fakeGenerator{cost: 53_000}, &fakeAIBilling{}, &fakeUploader{}
	out, err := NewImageUseCase(gen, fakeFunds{}, bill, up, 250_000).Generate(context.Background(), imageRequest())
	if err != nil {
		t.Fatal(err)
	}
	if out.Media.ID != "m-1" || out.Media.Type != media.MediaTypeProductImage || len(bill.events) != 1 || bill.events[0].cost != 53_000 {
		t.Fatalf("out %+v billed %+v", out, bill.events)
	}
}

func TestUnknownProviderCostIsBilledAtTheCeilingNeverFree(t *testing.T) {
	bill := &fakeAIBilling{}
	if _, err := NewImageUseCase(&fakeGenerator{}, fakeFunds{}, bill, &fakeUploader{}, 250_000).Generate(context.Background(), imageRequest()); err != nil {
		t.Fatal(err)
	}
	if bill.events[0].cost != 250_000 {
		t.Fatalf("billed %+v", bill.events)
	}
}

func TestNoGenerationWithoutFunds(t *testing.T) {
	gen := &fakeGenerator{}
	_, err := NewImageUseCase(gen, fakeFunds{err: errors.New("no balance")}, &fakeAIBilling{}, &fakeUploader{}, 250_000).Generate(context.Background(), imageRequest())
	if err == nil || gen.calls != 0 {
		t.Fatalf("err %v calls %d", err, gen.calls)
	}
}
