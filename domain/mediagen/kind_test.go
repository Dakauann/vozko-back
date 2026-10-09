package mediagen

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"vozko/domain/media"
)

const audioModel = "google/lyria-3-clip-preview"

func codesOf(t *testing.T, err error) map[string]string {
	t.Helper()
	var invalid *ValidationError
	if !errors.As(err, &invalid) {
		t.Fatalf("expected a validation error, got %v", err)
	}
	return invalid.Codes()
}

func validVideo() Request {
	return Request{
		WorkspaceID: "ws", Kind: KindVideo, Aspect: AspectStory,
		Video: SlideshowTimeline([]Scene{{MediaID: "img-1", Seconds: 4}, {MediaID: "img-2", Seconds: 6}}, "music-1", ""),
	}
}

func TestAKindIsRequiredAndMustBeKnown(t *testing.T) {
	if codesOf(t, Request{WorkspaceID: "ws", Model: testModel, Prompt: "x"}.Validate())[FieldKind] != CodeRequired {
		t.Fatal("missing kind accepted")
	}
	if codesOf(t, Request{WorkspaceID: "ws", Kind: "gif", Model: testModel, Prompt: "x"}.Validate())[FieldKind] != CodeUnknown {
		t.Fatal("unknown kind accepted")
	}
}

func TestMusicNeedsAPromptAndAModelButNoAspect(t *testing.T) {
	if err := (Request{WorkspaceID: "ws", Kind: KindMusic, Model: audioModel, Prompt: "lo-fi calmo para cafeteria"}).Validate(); err != nil {
		t.Fatal(err)
	}
	codes := codesOf(t, Request{WorkspaceID: "ws", Kind: KindMusic, Prompt: strings.Repeat("a", MaxMusicPromptRunes+1)}.Validate())
	if codes[FieldPrompt] != CodeTooLong || codes[FieldModel] != CodeRequired {
		t.Fatalf("codes %v", codes)
	}
}

func TestAVoiceOverNeedsAScriptAndAKnownVoiceToken(t *testing.T) {
	ok := Request{WorkspaceID: "ws", Kind: KindVoice, Model: "openai/gpt-audio-mini", Prompt: "Conheça a Vozko.", Voice: "alloy"}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	bad := ok
	bad.Voice, bad.Prompt = "Alloy; rm", ""
	codes := codesOf(t, bad.Validate())
	if codes[FieldVoice] != CodeUnknown || codes[FieldPrompt] != CodeRequired {
		t.Fatalf("codes %v", codes)
	}
}

func TestAVideoIsRenderedWithoutAModel(t *testing.T) {
	if err := validVideo().Validate(); err != nil {
		t.Fatal(err)
	}
	withModel := validVideo()
	withModel.Model = testModel
	if codesOf(t, withModel.Validate())[FieldModel] != CodeUnexpected {
		t.Fatal("a render does not use a model")
	}
}

func TestTheTimelineKeepsItsInvariants(t *testing.T) {
	cases := map[string]struct {
		change func(*Timeline)
		code   string
	}{
		"empty":           {func(tl *Timeline) { tl.Visual = []Track{{}}; tl.Audio = nil }, CodeRequired},
		"over 90 s":       {func(tl *Timeline) { tl.DurationMS = MaxVideoMS + 1 }, CodeTooLong},
		"bad background":  {func(tl *Timeline) { tl.Background = "red" }, CodeUnknown},
		"overlap":         {func(tl *Timeline) { tl.Visual[0].Clips[1].StartMS = 3_000 }, CodeOverlap},
		"past the end":    {func(tl *Timeline) { tl.Visual[0].Clips[1].DurationMS = 7_000 }, CodeOutOfRange},
		"fades too long":  {func(tl *Timeline) { tl.Audio[0].Clips[0].FadeInMS = 9_000 }, CodeOutOfRange},
		"too loud":        {func(tl *Timeline) { tl.Audio[0].Clips[0].Volume = 3 }, CodeOutOfRange},
		"unknown fit":     {func(tl *Timeline) { tl.Visual[0].Clips[0].Fit = "stretch" }, CodeUnknown},
		"off canvas":      {func(tl *Timeline) { tl.Visual[0].Clips[0].Transform.X = 1.5 }, CodeOutOfRange},
		"no media":        {func(tl *Timeline) { tl.Visual[0].Clips[0].MediaID = " " }, CodeRequired},
		"too many tracks": {func(tl *Timeline) { tl.Visual = make([]Track, MaxVisualTracks+1) }, CodeTooMany},
		"negative trim":   {func(tl *Timeline) { tl.Visual[0].Clips[0].TrimInMS = -1 }, CodeOutOfRange},
	}
	for name, c := range cases {
		req := validVideo()
		c.change(&req.Video)
		if got := codesOf(t, req.Validate())[FieldTimeline]; got != c.code {
			t.Errorf("%s: code %q", name, got)
		}
	}
}

func TestASlideshowLowersTheMusicUnderTheVoice(t *testing.T) {
	tl := SlideshowTimeline([]Scene{{MediaID: "a", Seconds: 2}, {MediaID: "b", Seconds: 3.5}}, "song", "voice")
	if tl.DurationMS != 5_500 || len(tl.Visual[0].Clips) != 2 || tl.Visual[0].Clips[1].StartMS != 2_000 {
		t.Fatalf("visual %+v", tl)
	}
	if len(tl.Audio) != 2 || tl.Audio[0].Clips[0].Volume != musicUnderVoice || tl.Audio[1].Clips[0].Volume != 1 || tl.Audio[0].Clips[0].FadeOutMS != musicFadeOutMS {
		t.Fatalf("audio %+v", tl.Audio)
	}
	if solo := SlideshowTimeline([]Scene{{MediaID: "a", Seconds: 2}}, "song", ""); solo.Audio[0].Clips[0].Volume != 1 {
		t.Fatalf("music alone plays at full volume: %+v", solo.Audio)
	}
	if SlideshowIssues(nil) != CodeRequired || SlideshowIssues([]Scene{{MediaID: "a", Seconds: 16}}) != CodeOutOfRange {
		t.Fatal("slideshow limits")
	}
}

func TestEveryInputOfAKindChangesItsFingerprint(t *testing.T) {
	seen := map[string]string{"base": validVideo().Fingerprint("u1")}
	variants := map[string]func(*Request){
		"clip length": func(r *Request) { r.Video.Visual[0].Clips[0].DurationMS = 3_000 },
		"trim":        func(r *Request) { r.Video.Visual[0].Clips[0].TrimInMS = 500 },
		"transform":   func(r *Request) { r.Video.Visual[0].Clips[0].Transform.X = 0.4 },
		"volume":      func(r *Request) { r.Video.Audio[0].Clips[0].Volume = 0.5 },
		"background":  func(r *Request) { r.Video.Background = "#ffffff" },
		"aspect":      func(r *Request) { r.Aspect = AspectSquare },
		"kind":        func(r *Request) { r.Kind, r.Prompt, r.Model = KindMusic, "x", audioModel },
	}
	for name, change := range variants {
		r := validVideo()
		change(&r)
		fp := r.Fingerprint("u1")
		for other, existing := range seen {
			if existing == fp {
				t.Fatalf("%s shares a fingerprint with %s", name, other)
			}
		}
		seen[name] = fp
	}
	voice := Request{WorkspaceID: "ws", Kind: KindVoice, Model: "m", Prompt: "Olá", Voice: "alloy"}
	other := voice
	other.Voice = "verse"
	if voice.Fingerprint("u1") == other.Fingerprint("u1") {
		t.Fatal("the voice is part of the fingerprint")
	}
}

func TestAJobKeepsOnlyTheInputsOfItsKind(t *testing.T) {
	job, err := NewJob(Request{WorkspaceID: "ws", Kind: KindMusic, Model: audioModel, Prompt: " samba ", Aspect: AspectStory, ReferenceMediaIDs: []string{"m-1"}, Voice: "alloy"}, "u1")
	if err != nil {
		t.Fatal(err)
	}
	want := Request{WorkspaceID: "ws", Kind: KindMusic, Model: audioModel, Prompt: "samba"}
	if !reflect.DeepEqual(job.Request(), want) {
		t.Fatalf("request %+v", job.Request())
	}
}

func TestSourcesAreTheMediaAKindReadsWithTheTypeEachNeeds(t *testing.T) {
	video := validVideo()
	video.Video.Visual[0].Clips[1].MediaID = "img-1"
	roles := video.SourceRoles()
	if len(roles) != 2 || roles[0].MediaID != "img-1" || roles[1].MediaID != "music-1" {
		t.Fatalf("each medium is read once: %+v", roles)
	}
	scene, music := roles[0], roles[1]
	if !scene.Accepted(media.MediaTypeProductVideo) || scene.Accepted(media.MediaTypeAudio) || !music.Accepted(media.MediaTypeAudio) || !music.Accepted(media.MediaTypeProductVideo) || music.Field != FieldTimeline {
		t.Fatalf("roles %+v", roles)
	}
	image := Request{Kind: KindImage, ReferenceMediaIDs: []string{" ref "}}.SourceRoles()
	if len(image) != 1 || image[0].MediaID != "ref" || image[0].Mismatch != CodeNotImage || image[0].Accepted(media.MediaTypeProductVideo) {
		t.Fatalf("image roles %+v", image)
	}
	if roles := (Request{Kind: KindMusic, ReferenceMediaIDs: []string{"x"}}).SourceRoles(); roles != nil {
		t.Fatalf("music reads no media, got %v", roles)
	}
}

func TestEachKindHasItsTopicAndStorage(t *testing.T) {
	if KindVideo.Topic() != RenderTopic || KindMusic.Topic() != Topic || KindImage.Topic() != Topic {
		t.Fatal("renders must not share the generation topic")
	}
	music, _ := KindMusic.Storage()
	video, _ := KindVideo.Storage()
	if music.Type != media.MediaTypeAudio || video.Type != media.MediaTypeProductVideo || video.Extension != ".mp4" {
		t.Fatalf("storage %+v %+v", music, video)
	}
	if _, ok := Kind("gif").Storage(); ok || KindVideo.UsesModel() || !KindVoice.UsesModel() {
		t.Fatal("kind rules")
	}
}

func TestAResultIsOnlyHandedOutOnceTheJobIsDone(t *testing.T) {
	job := &Job{Status: StatusSettling, MediaID: "m-1", MediaURL: "https://cdn/m-1.m4a"}
	if _, ok := job.Delivered(); ok {
		t.Fatal("a settling job handed out its media before being billed")
	}
	job.Status = StatusDone
	if result, ok := job.Delivered(); !ok || result.MediaID != "m-1" {
		t.Fatalf("result %+v ok %v", result, ok)
	}
	if _, ok := (&Job{Status: StatusFailed, MediaID: "m-1"}).Delivered(); ok {
		t.Fatal("a failed job handed out media")
	}
}

func TestTheDefaultModelIsTheFirstTheCatalogRanks(t *testing.T) {
	if got, ok := DefaultModel([]Model{{ID: "google/lyria-3-pro-preview"}, {ID: audioModel}}); !ok || got.ID != "google/lyria-3-pro-preview" {
		t.Fatalf("default %+v", got)
	}
	if _, ok := DefaultModel(nil); ok {
		t.Fatal("a default out of nothing")
	}
}

func TestOnlyAnEditingProxyIsReusedOnceDone(t *testing.T) {
	if !KindProxy.Reusable() {
		t.Fatal("a finished proxy of the same source serves every later edit")
	}
	for _, kind := range []Kind{KindImage, KindMusic, KindVoice, KindVideo, KindCutout, KindCaptions, KindDenoise} {
		if kind.Reusable() {
			t.Fatalf("%s must run again when asked again", kind)
		}
	}
}

func TestProcessingKindsReadOneSourceWithoutAModel(t *testing.T) {
	cases := map[Kind]struct {
		accepts media.MediaType
		refuses media.MediaType
		storage media.MediaType
	}{
		KindCutout:   {media.MediaTypeProductImage, media.MediaTypeProductVideo, media.MediaTypeProductImage},
		KindCaptions: {media.MediaTypeProductVideo, media.MediaTypeProductImage, media.MediaTypeDocument},
		KindDenoise:  {media.MediaTypeAudio, media.MediaTypeProductImage, media.MediaTypeAudio},
		KindProxy:    {media.MediaTypeProductVideo, media.MediaTypeAudio, media.MediaTypeStudioProxy},
	}
	for kind, c := range cases {
		req := Request{WorkspaceID: "ws", Kind: kind, SourceMediaID: " m-1 "}
		if err := req.Validate(); err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		roles := req.SourceRoles()
		if len(roles) != 1 || roles[0].MediaID != "m-1" || roles[0].Field != FieldSource || !roles[0].Accepted(c.accepts) || roles[0].Accepted(c.refuses) {
			t.Fatalf("%s roles %+v", kind, roles)
		}
		storage, _ := kind.Storage()
		if storage.Type != c.storage || !kind.Processing() || kind.UsesModel() || kind.Topic() != RenderTopic {
			t.Fatalf("%s rules", kind)
		}
		withModel := req
		withModel.Model = testModel
		if codesOf(t, withModel.Validate())[FieldModel] != CodeUnexpected {
			t.Fatalf("%s accepted a model", kind)
		}
		if codesOf(t, Request{WorkspaceID: "ws", Kind: kind}.Validate())[FieldSource] != CodeRequired {
			t.Fatalf("%s without a source", kind)
		}
	}
	a := Request{WorkspaceID: "ws", Kind: KindCutout, SourceMediaID: "m-1"}
	b := a
	b.SourceMediaID = "m-2"
	if a.Fingerprint("u") == b.Fingerprint("u") {
		t.Fatal("the source is part of the fingerprint")
	}
}
