package media_infra

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	media_domain "vozko/domain/media"
	"vozko/domain/mediagen"
)

const (
	maxRenderSourceBytes = 120 << 20
	maxFilterGraphBytes  = 100 << 10
	renderFPS            = 30
	videoMIMEType        = "video/mp4"
	renderModel          = "vozko/render"
)

var (
	errRenderSources = errors.New("render: sources do not match the timeline")
	errGraphTooLarge = errors.New("render: the timeline compiles to a filter graph that is too large")
)

type VideoRenderer struct {
	client *http.Client
}

var _ mediagen.Generator = (*VideoRenderer)(nil)

func NewVideoRenderer(client *http.Client) *VideoRenderer {
	return &VideoRenderer{client: client}
}

type stagedSource struct {
	path  string
	video bool
}

func (r *VideoRenderer) Generate(ctx context.Context, req mediagen.Request, sources []mediagen.Source) (*mediagen.Output, error) {
	size, err := req.Aspect.Size()
	if err != nil {
		return nil, err
	}
	dir, cleanup, err := workDir()
	if err != nil {
		return nil, err
	}
	defer cleanup()
	staged, err := r.stage(ctx, dir, sources)
	if err != nil {
		return nil, err
	}
	silent, err := silentVideos(ctx, req.Video, staged)
	if err != nil {
		return nil, err
	}
	output := filepath.Join(dir, "video.mp4")
	plan, err := compileTimeline(req.Video, size, staged, silent, output)
	if err != nil {
		return nil, err
	}
	for name, content := range plan.files {
		if _, err := writeInput(dir, name, []byte(content)); err != nil {
			return nil, err
		}
	}
	if err := runFFmpegIn(ctx, dir, plan.args...); err != nil {
		return nil, err
	}
	data, err := readOutput(output)
	if err != nil {
		return nil, err
	}
	return &mediagen.Output{Bytes: data, MIMEType: videoMIMEType, Model: renderModel}, nil
}

func (r *VideoRenderer) stage(ctx context.Context, dir string, sources []mediagen.Source) (map[string]stagedSource, error) {
	staged := make(map[string]stagedSource, len(sources))
	for _, source := range sources {
		if _, done := staged[source.MediaID]; done {
			continue
		}
		data, err := downloadBounded(ctx, r.client, source.URL, maxRenderSourceBytes)
		if err != nil {
			return nil, fmt.Errorf("render: download %s: %w", source.MediaID, err)
		}
		path, err := writeInput(dir, "source-"+strconv.Itoa(len(staged)), data)
		if err != nil {
			return nil, err
		}
		staged[source.MediaID] = stagedSource{path: path, video: source.Type == media_domain.MediaTypeProductVideo}
	}
	return staged, nil
}

func silentVideos(ctx context.Context, timeline mediagen.Timeline, staged map[string]stagedSource) (map[string]bool, error) {
	silent := map[string]bool{}
	for _, track := range timeline.Audio {
		for _, c := range track.Clips {
			source := staged[c.MediaID]
			if _, probed := silent[c.MediaID]; probed || !source.video {
				continue
			}
			has, err := hasAudioStream(ctx, source.path)
			if err != nil {
				return nil, err
			}
			silent[c.MediaID] = !has
		}
	}
	return silent, nil
}

func hasAudioStream(ctx context.Context, path string) (bool, error) {
	out, err := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-select_streams", "a", "-show_entries", "stream=index", "-of", "csv=p=0", path).Output()
	if err != nil {
		return false, fmt.Errorf("render: probe %s: %w", filepath.Base(path), err)
	}
	return strings.TrimSpace(string(out)) != "", nil
}

func ms(v int64) string {
	return strconv.FormatFloat(float64(v)/1000, 'f', 3, 64)
}

func even(v float64) int {
	n := int(v + 0.5)
	if n%2 == 1 {
		n++
	}
	return max(n, 2)
}

type renderPlan struct {
	args  []string
	files map[string]string
}

func compileTimeline(timeline mediagen.Timeline, size mediagen.Size, staged map[string]stagedSource, silent map[string]bool, output string) (renderPlan, error) {
	var args, graph []string
	files := map[string]string{}
	input := 0
	total := ms(timeline.DurationMS)
	graph = append(graph, fmt.Sprintf("color=c=0x%s:s=%dx%d:r=%d:d=%s,format=rgba[base0]", strings.TrimPrefix(timeline.Background, "#"), size.Width, size.Height, renderFPS, total))
	last := "base0"
	for _, track := range timeline.Visual {
		for _, c := range track.Clips {
			source, ok := staged[c.MediaID]
			if !ok {
				return renderPlan{}, errRenderSources
			}
			if source.video {
				args = append(args, "-ss", ms(c.TrimInMS), "-t", ms(c.DurationMS), "-i", source.path)
			} else {
				args = append(args, "-loop", "1", "-t", ms(c.DurationMS), "-i", source.path)
			}
			clip, next := "c"+strconv.Itoa(input), "base"+strconv.Itoa(input+1)
			x, y := overlayPosition(c)
			if k := keyframesOf(c); animated(k.Opacity) {
				files[opacityCommandFile(input)] = opacityCommands(k.Opacity, c.DurationMS, opacityFilter(input))
			}
			graph = append(graph, visualChain(input, c, size, clip), fmt.Sprintf(
				"[%s][%s]overlay=x='%s':y='%s':eof_action=pass:enable='between(t,%s,%s)'[%s]",
				last, clip, x, y, ms(c.StartMS), ms(c.EndMS()), next,
			))
			last = next
			input++
		}
	}
	graph = append(graph, fmt.Sprintf("[%s]format=yuv420p[vout]", last))
	mix := []string{"[silence]"}
	graph = append(graph, fmt.Sprintf("anullsrc=r=48000:cl=stereo,atrim=duration=%s[silence]", total))
	for _, track := range timeline.Audio {
		for _, c := range track.Clips {
			source, ok := staged[c.MediaID]
			if !ok {
				return renderPlan{}, errRenderSources
			}
			if silent[c.MediaID] {
				continue
			}
			args = append(args, "-ss", ms(c.TrimInMS), "-t", ms(c.DurationMS), "-i", source.path)
			label := "a" + strconv.Itoa(input)
			graph = append(graph, audioChain(input, c, label))
			mix = append(mix, "["+label+"]")
			input++
		}
	}
	graph = append(graph, fmt.Sprintf("%samix=inputs=%d:duration=first:normalize=0,alimiter=limit=0.95,atrim=duration=%s[aout]", strings.Join(mix, ""), len(mix), total))
	filter := strings.Join(graph, ";")
	if len(filter) > maxFilterGraphBytes {
		return renderPlan{}, errGraphTooLarge
	}
	return renderPlan{files: files, args: append(args,
		"-filter_complex", filter,
		"-map", "[vout]", "-map", "[aout]",
		"-c:v", "libx264", "-preset", "veryfast", "-crf", "20", "-profile:v", "high", "-pix_fmt", "yuv420p", "-r", strconv.Itoa(renderFPS),
		"-c:a", "aac", "-b:a", "192k", "-ar", "48000", "-ac", "2",
		"-t", total,
		"-movflags", "+faststart",
		"-y", output,
	)}, nil
}

func fraction(v float64) string {
	return strconv.FormatFloat(v, 'f', 6, 64)
}

func visualChain(input int, c mediagen.Clip, size mediagen.Size, label string) string {
	w, h := even(c.Transform.W*float64(size.Width)), even(c.Transform.H*float64(size.Height))
	steps := []string{fmt.Sprintf("[%d:v]format=rgba", input)}
	if c.Fit == mediagen.FitContain {
		steps = append(steps, fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2:color=black@0", w, h, w, h))
	} else {
		steps = append(steps, fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=increase,crop=%d:%d", w, h, w, h))
	}
	steps = append(steps, "setsar=1", fmt.Sprintf("fps=%d", renderFPS))
	steps = append(steps, motionSteps(input, c, w, h)...)
	if c.FadeInMS > 0 {
		steps = append(steps, fmt.Sprintf("fade=t=in:st=0:d=%s:alpha=1", ms(c.FadeInMS)))
	}
	if c.FadeOutMS > 0 {
		steps = append(steps, fmt.Sprintf("fade=t=out:st=%s:d=%s:alpha=1", ms(c.DurationMS-c.FadeOutMS), ms(c.FadeOutMS)))
	}
	steps = append(steps, fmt.Sprintf("trim=duration=%s,setpts=PTS-STARTPTS+%s/TB", ms(c.DurationMS), ms(c.StartMS)))
	return strings.Join(steps, ",") + "[" + label + "]"
}

func audioChain(input int, c mediagen.Clip, label string) string {
	steps := []string{
		fmt.Sprintf("[%d:a]aresample=48000,aformat=channel_layouts=stereo", input),
		fmt.Sprintf("atrim=duration=%s,asetpts=PTS-STARTPTS", ms(c.DurationMS)),
		fmt.Sprintf("volume=%s", fraction(c.Volume)),
	}
	if c.FadeInMS > 0 {
		steps = append(steps, fmt.Sprintf("afade=t=in:st=0:d=%s", ms(c.FadeInMS)))
	}
	if c.FadeOutMS > 0 {
		steps = append(steps, fmt.Sprintf("afade=t=out:st=%s:d=%s", ms(c.DurationMS-c.FadeOutMS), ms(c.FadeOutMS)))
	}
	steps = append(steps, fmt.Sprintf("adelay=delays=%d|%d", c.StartMS, c.StartMS))
	return strings.Join(steps, ",") + "[" + label + "]"
}

func overlayPosition(c mediagen.Clip) (string, string) {
	k := keyframesOf(c)
	x := centerExpr(c.Transform.X, k.X, c.StartMS) + "*W-overlay_w/2"
	y := centerExpr(c.Transform.Y, k.Y, c.StartMS) + "*H-overlay_h/2"
	add := func(m *mediagen.Motion, progress string) {
		if m == nil {
			return
		}
		dx, dy := m.Edge.Direction()
		ease := "*pow(1-clip(" + progress + ",0,1),3)"
		if dx != 0 {
			x += signed(dx*mediagen.MotionDistance) + "*W" + ease
		}
		if dy != 0 {
			y += signed(dy*mediagen.MotionDistance) + "*H" + ease
		}
	}
	if c.MotionIn != nil {
		add(c.MotionIn, "(t-"+ms(c.StartMS)+")/"+ms(c.MotionIn.DurationMS))
	}
	if c.MotionOut != nil {
		add(c.MotionOut, "("+ms(c.EndMS())+"-t)/"+ms(c.MotionOut.DurationMS))
	}
	return x, y
}

func signed(v float64) string {
	if v < 0 {
		return fraction(v)
	}
	return "+" + fraction(v)
}
