package media_usecase_test

import (
	"errors"
	"testing"
	"time"

	"vozko/domain/media"
	workspace_plan "vozko/domain/workspace/workspace_plan"
	media_usecase "vozko/usecases/media"
)

type fakeMediaRepo struct {
	countByType map[string]int64
	byID        map[string]*media.Media
	deleted     []string
	created     []*media.Media
}

func (f *fakeMediaRepo) CreateMedia(m *media.Media) error {
	f.created = append(f.created, m)
	return nil
}
func (f *fakeMediaRepo) ListMediasByWorkspace(string) ([]media.Media, error) { return nil, nil }
func (f *fakeMediaRepo) DeleteMedia(id string) error {
	f.deleted = append(f.deleted, id)
	return nil
}
func (f *fakeMediaRepo) CountWorkspaceUploadsToday(string) (int64, error) { return 0, nil }
func (f *fakeMediaRepo) GetMediaByID(id string) (*media.Media, error) {
	if m, ok := f.byID[id]; ok {
		return m, nil
	}
	return nil, errors.New("record not found")
}
func (f *fakeMediaRepo) GetMediasByIDs([]string) ([]media.Media, error) { return nil, nil }
func (f *fakeMediaRepo) MediaExists(string) (bool, error)               { return false, nil }
func (f *fakeMediaRepo) CountByWorkspaceID(string) (int64, error)       { return 0, nil }
func (f *fakeMediaRepo) CountByWorkspaceIDAndType(ws string, t media.MediaType) (int64, error) {
	return f.countByType[ws+"|"+string(t)], nil
}

type fakeSubs struct{ sub *workspace_plan.WorkspaceSubscription }

func (f fakeSubs) GetCurrentByWorkspaceID(string, time.Time) (*workspace_plan.WorkspaceSubscription, error) {
	return f.sub, nil
}

type fakePlans struct{ plan *workspace_plan.PlanDefinition }

func (f fakePlans) GetByID(string) (*workspace_plan.PlanDefinition, error) { return f.plan, nil }

func activeSub() *workspace_plan.WorkspaceSubscription {
	return &workspace_plan.WorkspaceSubscription{
		PlanDefinitionID: "plan-1",
		Status:           workspace_plan.SubscriptionStatusActive,
	}
}

func TestHoldMusicGate_PlanDriven(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		sub     *workspace_plan.WorkspaceSubscription
		planMax int
		count   int64
		want    error
	}{
		{"no subscription", nil, 5, 0, media.ErrHoldMusicNotIncluded},
		{"plan grants zero", activeSub(), 0, 0, media.ErrHoldMusicNotIncluded},
		{"under the plan limit", activeSub(), 3, 2, nil},
		{"at the plan limit", activeSub(), 3, 3, media.ErrHoldMusicQuotaReached},
		{"plan above the hard cap is clamped to 10", activeSub(), 50, 10, media.ErrHoldMusicQuotaReached},
		{"clamped limit still allows under 10", activeSub(), 50, 9, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			repo := &fakeMediaRepo{countByType: map[string]int64{
				"ws-1|" + string(media.MediaTypeHoldMusic): c.count,
			}}
			gate := media_usecase.NewHoldMusicQuotaGate(
				fakeSubs{sub: c.sub},
				fakePlans{plan: &workspace_plan.PlanDefinition{MaxHoldMusicTracks: c.planMax}},
				repo,
			)
			err := gate.CanUploadHoldMusic("ws-1")
			if !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
		})
	}
}

// --- upload standardization path -----------------------------------------

type fakeTranscoder struct{ fail bool }

func (f fakeTranscoder) ToHoldMusicMP3(in []byte) ([]byte, error) {
	if f.fail {
		return nil, errors.New("not audio")
	}
	return []byte("MP3:" + string(in)), nil
}

type fakeStorage struct {
	names map[string][]byte
}

func (f *fakeStorage) UploadFile(name string, data []byte) error {
	if f.names == nil {
		f.names = map[string][]byte{}
	}
	f.names[name] = data
	return nil
}
func (f *fakeStorage) GetFileURL(name string) string { return "https://cdn.test/" + name }

type allowGate struct{ err error }

func (g allowGate) CanUploadHoldMusic(string) error { return g.err }

func TestUploadMedia_HoldMusicIsGatedAndStandardized(t *testing.T) {
	t.Parallel()
	repo := &fakeMediaRepo{}
	storage := &fakeStorage{}
	uc := media_usecase.NewUploadMediaUseCase(repo, storage, fakeTranscoder{}, allowGate{})

	out, err := uc.UploadMedia("ws-1", []byte("rawwav"), "abc.wav", media.MediaTypeHoldMusic, "Minha música")
	if err != nil {
		t.Fatalf("UploadMedia: %v", err)
	}
	stored, ok := storage.names["abc.mp3"]
	if !ok {
		t.Fatalf("hold music must be stored as .mp3; stored keys: %v", storage.names)
	}
	if string(stored) != "MP3:rawwav" {
		t.Fatalf("stored bytes must be the TRANSCODED output, got %q", stored)
	}
	if out.Type != media.MediaTypeHoldMusic {
		t.Fatalf("type = %s", out.Type)
	}
}

func TestUploadMedia_HoldMusicQuotaDenied(t *testing.T) {
	t.Parallel()
	uc := media_usecase.NewUploadMediaUseCase(&fakeMediaRepo{}, &fakeStorage{}, fakeTranscoder{}, allowGate{err: media.ErrHoldMusicQuotaReached})
	_, err := uc.UploadMedia("ws-1", []byte("x"), "a.mp3", media.MediaTypeHoldMusic, "d")
	if !errors.Is(err, media.ErrHoldMusicQuotaReached) {
		t.Fatalf("err = %v, want quota error", err)
	}
}

func TestUploadMedia_HoldMusicFailsClosedWithoutGate(t *testing.T) {
	t.Parallel()
	uc := media_usecase.NewUploadMediaUseCase(&fakeMediaRepo{}, &fakeStorage{}, nil, nil)
	_, err := uc.UploadMedia("ws-1", []byte("x"), "a.mp3", media.MediaTypeHoldMusic, "d")
	if !errors.Is(err, media.ErrHoldMusicNotIncluded) {
		t.Fatalf("err = %v, want ErrHoldMusicNotIncluded", err)
	}
}

func TestUploadMedia_OtherTypesBypassHoldMusicPipeline(t *testing.T) {
	t.Parallel()
	storage := &fakeStorage{}
	uc := media_usecase.NewUploadMediaUseCase(&fakeMediaRepo{}, storage, fakeTranscoder{fail: true}, allowGate{err: errors.New("must not be called")})
	if _, err := uc.UploadMedia("ws-1", []byte("plain"), "doc.mp3", media.MediaTypeAudio, "d"); err != nil {
		t.Fatalf("plain audio upload must not touch the hold music pipeline: %v", err)
	}
	if _, ok := storage.names["doc.mp3"]; !ok {
		t.Fatal("plain audio must be stored unmodified under its original name")
	}
}
