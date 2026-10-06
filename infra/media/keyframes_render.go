package media_infra

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"vozko/domain/mediagen"
)

func seconds(v int64) string {
	return ms(v)
}

func easingExpr(e mediagen.Easing, p string) string {
	switch e {
	case mediagen.EaseHold:
		return "0"
	case mediagen.EaseIn:
		return "pow(" + p + ",3)"
	case mediagen.EaseOut:
		return "(1-pow(1-" + p + ",3))"
	case mediagen.EaseInOut:
		return "if(lt(" + p + ",0.5),4*pow(" + p + ",3),1-pow(-2*" + p + "+2,3)/2)"
	}
	return p
}

func keyframeExpr(frames []mediagen.Keyframe, at string) string {
	last := frames[len(frames)-1]
	expr := fraction(last.Value)
	for i := len(frames) - 2; i >= 0; i-- {
		from, to := frames[i], frames[i+1]
		progress := "((" + at + "-" + seconds(from.AtMS) + ")/" + seconds(to.AtMS-from.AtMS) + ")"
		segment := fraction(from.Value) + "+" + fraction(to.Value-from.Value) + "*" + easingExpr(from.Easing, progress)
		expr = "if(lt(" + at + "," + seconds(to.AtMS) + ")," + segment + "," + expr + ")"
	}
	if len(frames) > 1 {
		expr = "if(lt(" + at + "," + seconds(frames[0].AtMS) + ")," + fraction(frames[0].Value) + "," + expr + ")"
	}
	return expr
}

func animated(frames []mediagen.Keyframe) bool {
	return len(frames) > 0
}

func keyframesOf(c mediagen.Clip) mediagen.Keyframes {
	if c.Keyframes == nil {
		return mediagen.Keyframes{}
	}
	return *c.Keyframes
}

func opacityCommandFile(input int) string {
	return "opacity-" + strconv.Itoa(input) + ".cmd"
}

func opacityFilter(input int) string {
	return "colorchannelmixer@opacity" + strconv.Itoa(input)
}

func opacityCommands(frames []mediagen.Keyframe, durationMS int64, target string) string {
	var b strings.Builder
	last := math.NaN()
	count := int(math.Ceil(float64(durationMS) / 1000 * renderFPS))
	for n := 0; n <= count; n++ {
		at := float64(n) / renderFPS
		value := math.Round(mediagen.ValueAt(frames, at*1000)*1e4) / 1e4
		if value == last {
			continue
		}
		last = value
		fmt.Fprintf(&b, "%.4f %s aa %.4f;\n", math.Max(0, at-0.5/renderFPS), target, value)
	}
	return b.String()
}

type animatedBox struct {
	width, height int
	pad           bool
}

func boxFor(c mediagen.Clip, w, h int) animatedBox {
	k := keyframesOf(c)
	if !animated(k.Scale) && !animated(k.Rotation) {
		return animatedBox{width: w, height: h}
	}
	scale := c.Keyframes.MaxScale()
	sw, sh := float64(w)*scale, float64(h)*scale
	if animated(k.Rotation) || c.Transform.Rotation != 0 {
		side := even(math.Hypot(sw, sh))
		return animatedBox{width: side, height: side, pad: true}
	}
	return animatedBox{width: even(sw), height: even(sh), pad: true}
}

func motionSteps(input int, c mediagen.Clip, w, h int) []string {
	k := keyframesOf(c)
	box := boxFor(c, w, h)
	var steps []string
	if animated(k.Opacity) {
		steps = append(steps, "sendcmd=f="+opacityCommandFile(input), fmt.Sprintf("%s=aa=%s", opacityFilter(input), fraction(k.Opacity[0].Value)))
	} else if c.Transform.Opacity < 1 {
		steps = append(steps, fmt.Sprintf("colorchannelmixer=aa=%s", fraction(c.Transform.Opacity)))
	}
	if animated(k.Scale) {
		scale := keyframeExpr(k.Scale, "t")
		steps = append(steps,
			fmt.Sprintf("scale=w='max(2,trunc(%d*(%s)/2)*2)':h='max(2,trunc(%d*(%s)/2)*2)':eval=frame", w, scale, h, scale),
			fmt.Sprintf("pad=w=%d:h=%d:x='(ow-iw)/2':y='(oh-ih)/2':color=black@0:eval=frame", box.width, box.height),
		)
	}
	switch {
	case animated(k.Rotation):
		steps = append(steps, fmt.Sprintf("rotate=a='(%s)*PI/180':c=none:ow=%d:oh=%d", keyframeExpr(k.Rotation, "t"), box.width, box.height))
	case c.Transform.Rotation != 0 && box.pad:
		steps = append(steps, fmt.Sprintf("rotate=%s*PI/180:c=none:ow=%d:oh=%d", fraction(c.Transform.Rotation), box.width, box.height))
	case c.Transform.Rotation != 0:
		angle := fraction(c.Transform.Rotation)
		steps = append(steps, fmt.Sprintf("rotate=%s*PI/180:c=none:ow=rotw(%s*PI/180):oh=roth(%s*PI/180)", angle, angle, angle))
	}
	return steps
}

func centerExpr(static float64, frames []mediagen.Keyframe, startMS int64) string {
	if !animated(frames) {
		return fraction(static)
	}
	return "(" + keyframeExpr(frames, "(t-"+seconds(startMS)+")") + ")"
}
