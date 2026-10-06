package copilottools

import (
	"context"
	"fmt"
	"strconv"

	"vozko/domain/copilot"
	"vozko/domain/media"
	"vozko/domain/mediagen"
	"vozko/domain/tools"
)

const PreviewVideoPlan = "video_plan"

type videoSceneArgs struct {
	MediaID string  `json:"media_id" req:"true" id:"true" desc:"media_id de uma imagem ou vídeo da biblioteca (anexo do usuário, generate_image ou criativo)"`
	Seconds float64 `json:"seconds" req:"true" desc:"quanto tempo a cena fica na tela, de 1 a 15 segundos"`
}

type renderVideoArgs struct {
	Aspect       string           `json:"aspect" req:"true" enum:"square,portrait,story,landscape" desc:"square (1:1 feed), portrait (4:5 feed), story (9:16 stories e reels) ou landscape (16:9 YouTube e site)"`
	Scenes       []videoSceneArgs `json:"scenes" req:"true" desc:"de 1 a 10 cenas na ordem em que aparecem, cada uma de 1 a 15 segundos"`
	MusicMediaID string           `json:"music_media_id" id:"true" desc:"media_id de uma música da biblioteca (generate_music); fica mais baixa quando há locução"`
	VoiceMediaID string           `json:"voice_media_id" id:"true" desc:"media_id de uma locução da biblioteca (generate_voiceover)"`
}

func (a renderVideoArgs) scenes() []mediagen.Scene {
	scenes := make([]mediagen.Scene, 0, len(a.Scenes))
	for _, s := range a.Scenes {
		scenes = append(scenes, mediagen.Scene{MediaID: s.MediaID, Seconds: s.Seconds})
	}
	return scenes
}

func (a renderVideoArgs) request(cc copilot.Context) mediagen.Request {
	timeline := mediagen.SlideshowTimeline(a.scenes(), a.MusicMediaID, a.VoiceMediaID)
	return mediagen.Request{Kind: mediagen.KindVideo, WorkspaceID: cc.WorkspaceID, Aspect: mediagen.Aspect(a.Aspect), Video: timeline}
}

type VideoPlanPreview struct {
	Aspect string                 `json:"aspect"`
	Scenes []VideoScenePreview    `json:"scenes"`
	Music  *ImageReferencePreview `json:"music,omitempty"`
	Voice  *ImageReferencePreview `json:"voice,omitempty"`
}

type VideoScenePreview struct {
	MediaID string  `json:"mediaId"`
	URL     string  `json:"url"`
	Kind    string  `json:"kind"`
	Seconds float64 `json:"seconds"`
}

type renderVideoTool struct{ media MediaGeneration }

func NewRenderVideoTool(media MediaGeneration) copilot.Tool {
	return &renderVideoTool{media: media}
}

func (t *renderVideoTool) Meta() copilot.Meta { return mediaMeta() }

func (t *renderVideoTool) Definition() tools.Definition {
	return definition("render_video",
		"Monta um vídeo curto com som a partir de imagens ou vídeos da biblioteca, com música (generate_music) e ou locução (generate_voiceover), "+
			"no formato do posicionamento. Salva na biblioteca e devolve media_id e media_url; o media_id vai em create_ad, save_ad_draft ou swap_ad_creative "+
			"como criativo VIDEO, a única forma de um anúncio ter som. Não usa IA nem é cobrado; só depois da aprovação do usuário.",
		renderVideoArgs{})
}

func (t *renderVideoTool) Validate(ctx context.Context, cc copilot.Context, args map[string]interface{}) error {
	a, err := validateArgs[renderVideoArgs](nil, cc, args)
	if err != nil {
		return err
	}
	if code := mediagen.SlideshowIssues(a.scenes()); code != "" {
		return fmt.Errorf("%w: scenes %s", errInvalidArgs, code)
	}
	return checkMedia(ctx, t.media, a.request(cc))
}

func (t *renderVideoTool) Describe(_ context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	var a renderVideoArgs
	bindArgs(args, &a)
	req := a.request(cc)
	fields := []copilot.Field{
		{Key: "format", Value: a.Aspect},
		{Key: "scenes", Value: strconv.Itoa(len(a.Scenes)) + " cenas, " + secondsText(float64(req.Video.DurationMS)/1000)},
	}
	fields = append(fields, copilot.Field{Key: "sound", Value: soundText(a.MusicMediaID != "", a.VoiceMediaID != "")})
	return append(fields, copilot.Field{Key: "cost", Value: "sem custo"})
}

func soundText(music, voice bool) string {
	switch {
	case music && voice:
		return "música e locução"
	case voice:
		return "locução"
	}
	return "música"
}

func (t *renderVideoTool) Preview(_ context.Context, cc copilot.Context, args map[string]interface{}) *copilot.Preview {
	var a renderVideoArgs
	bindArgs(args, &a)
	req := a.request(cc)
	sources, err := t.media.Sources(req)
	if err != nil {
		return nil
	}
	byID := make(map[string]mediagen.Source, len(sources))
	for _, s := range sources {
		byID[s.MediaID] = s
	}
	plan := VideoPlanPreview{Aspect: a.Aspect}
	for _, scene := range a.scenes() {
		source := byID[scene.MediaID]
		kind := "image"
		if source.Type == media.MediaTypeProductVideo {
			kind = "video"
		}
		plan.Scenes = append(plan.Scenes, VideoScenePreview{MediaID: scene.MediaID, URL: source.URL, Kind: kind, Seconds: scene.Seconds})
	}
	if s, ok := byID[a.MusicMediaID]; ok {
		plan.Music = &ImageReferencePreview{MediaID: s.MediaID, URL: s.URL}
	}
	if s, ok := byID[a.VoiceMediaID]; ok {
		plan.Voice = &ImageReferencePreview{MediaID: s.MediaID, URL: s.URL}
	}
	return &copilot.Preview{Kind: PreviewVideoPlan, Data: plan}
}

func (t *renderVideoTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a renderVideoArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	return generateMedia(ctx, t.media, cc, "render_video", a.request(cc))
}
