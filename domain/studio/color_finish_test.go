package studio

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"vozko/domain/mediagen"
)

func TestTheEditorColorDocumentPassesTheContract(t *testing.T) {
	raw, err := os.ReadFile("testdata/editor_color.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateDocument(KindVideo, raw); err != nil {
		t.Fatalf("the editor saved curves, a secondary, a LUT, grade keys or a speed curve the backend refuses: %v", err)
	}
}

func TestCurvesSecondaryAndLutAreCheckedInsideTheGrade(t *testing.T) {
	good := ColorGrade{
		Curves: &ColorCurves{Master: []CurvePoint{{0, 0}, {0.4, 0.5}, {1, 1}}},
		HSL:    &HueSecondary{Hue: 120, Width: 30, Softness: ptr(20.0), Desaturate: ptr(100.0)},
		Lut:    &GradeLut{AssetID: "look", Intensity: ptr(0.5)},
	}
	if code := good.issue(); code != "" {
		t.Fatalf("a full grade refused: %s", code)
	}
	cases := map[string]ColorGrade{
		"a curve with one point":          {Curves: &ColorCurves{Red: []CurvePoint{{0.5, 0.5}}}},
		"a curve point past 1":            {Curves: &ColorCurves{Master: []CurvePoint{{0, 0}, {1, 1.2}}}},
		"curve points out of order":       {Curves: &ColorCurves{Blue: []CurvePoint{{0, 0}, {0.6, 0.4}, {0.6, 0.7}, {1, 1}}}},
		"an empty curve":                  {Curves: &ColorCurves{Green: []CurvePoint{}}},
		"a hue past a full turn":          {HSL: &HueSecondary{Hue: 400, Width: 30}},
		"a hue shift past half a turn":    {HSL: &HueSecondary{Hue: 10, Width: 30, Shift: ptr(200.0)}},
		"a desaturation past 100":         {HSL: &HueSecondary{Hue: 10, Width: 30, Desaturate: ptr(120.0)}},
		"a softness past 180":             {HSL: &HueSecondary{Hue: 10, Width: 30, Softness: ptr(181.0)}},
		"a LUT without a library file":    {Lut: &GradeLut{AssetID: " "}},
		"a LUT intensity past one":        {Lut: &GradeLut{AssetID: "look", Intensity: ptr(1.5)}},
		"a secondary saturation past 100": {HSL: &HueSecondary{Hue: 10, Width: 30, Saturation: ptr(-101.0)}},
	}
	for name, grade := range cases {
		if grade.issue() == "" {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestGradientMapsAndGradeKeysLiveOnMediaClipsOnly(t *testing.T) {
	gradient := &GradientMap{Stops: []GradientStop{{At: 0, Color: "#000000"}, {At: 1, Color: "#ffffff"}}, Intensity: 1}
	exposure := []mediagen.Keyframe{{AtMS: 0, Value: -1, Easing: mediagen.EaseLinear}, {AtMS: 1000, Value: 1, Easing: mediagen.EaseLinear}}
	doc := videoDoc()
	doc.Tracks[0].Clips[1].GradientMap = gradient
	doc.Tracks[0].Clips[1].Keyframes = &ClipKeyframes{GradeKeyframes: GradeKeyframes{Exposure: exposure}}
	doc.Tracks[0].Clips[0].Keyframes = &ClipKeyframes{GradeKeyframes: GradeKeyframes{Vibrance: []mediagen.Keyframe{{AtMS: 0, Value: 40, Easing: mediagen.EaseLinear}}}}
	if err := ValidateDocument(KindVideo, mustJSON(t, doc)); err != nil {
		t.Fatalf("a gradient map or grade keys on media refused: %v", err)
	}
	cases := map[string]func(*VideoDocument){
		"a gradient map on a text": func(d *VideoDocument) { d.Tracks[1].Clips[0].GradientMap = gradient },
		"a gradient map on a song": func(d *VideoDocument) { d.Tracks[2].Clips[0].GradientMap = gradient },
		"grade keys on a text": func(d *VideoDocument) {
			d.Tracks[1].Clips[0].Keyframes = &ClipKeyframes{GradeKeyframes: GradeKeyframes{Exposure: exposure}}
		},
		"grade keys on a song": func(d *VideoDocument) {
			d.Tracks[2].Clips[0].Keyframes = &ClipKeyframes{GradeKeyframes: GradeKeyframes{Exposure: exposure}}
		},
		"exposure keys past 5 stops": func(d *VideoDocument) {
			d.Tracks[0].Clips[1].Keyframes = &ClipKeyframes{GradeKeyframes: GradeKeyframes{Exposure: []mediagen.Keyframe{{AtMS: 0, Value: 6, Easing: mediagen.EaseLinear}}}}
		},
		"tint keys past 100": func(d *VideoDocument) {
			d.Tracks[0].Clips[1].Keyframes = &ClipKeyframes{GradeKeyframes: GradeKeyframes{Tint: []mediagen.Keyframe{{AtMS: 0, Value: 101, Easing: mediagen.EaseLinear}}}}
		},
	}
	for name, change := range cases {
		d := videoDoc()
		change(&d)
		if codeOf(t, ValidateDocument(KindVideo, mustJSON(t, d)))[FieldTracks] == "" {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestSpeedCurvesAndFrameMix(t *testing.T) {
	ramp := []mediagen.Keyframe{{AtMS: 0, Value: 1, Easing: mediagen.EaseInOut}, {AtMS: 2000, Value: 0.25, Easing: mediagen.EaseLinear}}
	doc := videoDoc()
	doc.Tracks[0].Clips[1].Keyframes = &ClipKeyframes{Speed: ramp}
	doc.Tracks[0].Clips[1].FrameMix = true
	doc.Tracks[2].Clips[0].Keyframes = &ClipKeyframes{Speed: ramp}
	if err := ValidateDocument(KindVideo, mustJSON(t, doc)); err != nil {
		t.Fatalf("a speed curve with frame mix refused: %v", err)
	}
	speed := 2.0
	cases := map[string]func(*VideoDocument){
		"a speed curve on an image": func(d *VideoDocument) { d.Tracks[0].Clips[0].Keyframes = &ClipKeyframes{Speed: ramp} },
		"a speed curve on a text":   func(d *VideoDocument) { d.Tracks[1].Clips[0].Keyframes = &ClipKeyframes{Speed: ramp} },
		"a speed curve with a speed": func(d *VideoDocument) {
			d.Tracks[0].Clips[1].Keyframes, d.Tracks[0].Clips[1].Speed = &ClipKeyframes{Speed: ramp}, &speed
		},
		"a speed curve on a freeze": func(d *VideoDocument) {
			d.Tracks[0].Clips[1].Keyframes, d.Tracks[0].Clips[1].Freeze = &ClipKeyframes{Speed: ramp}, true
		},
		"a speed key below 0.1": func(d *VideoDocument) {
			d.Tracks[0].Clips[1].Keyframes = &ClipKeyframes{Speed: []mediagen.Keyframe{{AtMS: 0, Value: 0.05, Easing: mediagen.EaseLinear}}}
		},
		"a curve that dips below 0.1 between keys": func(d *VideoDocument) {
			d.Tracks[0].Clips[1].Keyframes = &ClipKeyframes{Speed: []mediagen.Keyframe{{AtMS: 0, Value: 2, Easing: mediagen.EaseBackOut}, {AtMS: 1000, Value: 0.1, Easing: mediagen.EaseLinear}}}
		},
		"frame mix on an image": func(d *VideoDocument) { d.Tracks[0].Clips[0].FrameMix = true },
		"frame mix on a song":   func(d *VideoDocument) { d.Tracks[2].Clips[0].FrameMix = true },
		"frame mix on a freeze": func(d *VideoDocument) { d.Tracks[0].Clips[1].FrameMix, d.Tracks[0].Clips[1].Freeze = true, true },
	}
	for name, change := range cases {
		d := videoDoc()
		change(&d)
		if codeOf(t, ValidateDocument(KindVideo, mustJSON(t, d)))[FieldTracks] == "" {
			t.Errorf("%s accepted", name)
		}
	}
}

func cube(size int, extra string) []byte {
	var b strings.Builder
	b.WriteString("# a test look\nTITLE \"Teste\"\n" + extra + fmt.Sprintf("LUT_3D_SIZE %d\n", size))
	for i := 0; i < size*size*size; i++ {
		b.WriteString("0.25 0.5 0.75\n")
	}
	return []byte(b.String())
}

func TestCubeFilesAreParsedStrictly(t *testing.T) {
	info, err := ParseCube(cube(3, "DOMAIN_MIN 0 0 0\nDOMAIN_MAX 1 1 1\n"))
	if err != nil || info.Size != 3 || info.Title != "Teste" {
		t.Fatalf("a valid cube = %+v, %v", info, err)
	}
	if _, err := ParseCube(cube(2, "LUT_3D_INPUT_RANGE 0 1\n")); err != nil {
		t.Fatalf("an input range cube refused: %v", err)
	}
	cases := map[string]struct {
		raw  []byte
		want error
	}{
		"nothing":         {[]byte("# empty\n"), ErrCubeEmpty},
		"a 1D table":      {[]byte("LUT_1D_SIZE 4\n0 0 0\n"), ErrCubeOneDimensional},
		"size 1":          {cube(1, ""), ErrCubeSize},
		"size 66":         {[]byte("LUT_3D_SIZE 66\n"), ErrCubeSize},
		"missing rows":    {[]byte("LUT_3D_SIZE 2\n0 0 0\n0 0 0\n"), ErrCubeRows},
		"a word in a row": {[]byte("LUT_3D_SIZE 2\n0 0 x\n"), ErrCubeNumber},
		"two numbers":     {[]byte("LUT_3D_SIZE 2\n0 0\n"), ErrCubeNumber},
		"an empty domain": {cube(2, "DOMAIN_MIN 0 0 1\nDOMAIN_MAX 1 1 1\n"), ErrCubeDomain},
		"unknown keyword": {cube(2, "LUT_SHAPE warm\n"), ErrCubeKeyword},
		"keyword late":    {append(cube(2, ""), []byte("TITLE \"late\"\n")...), ErrCubeKeyword},
		"no size":         {[]byte("0 0 0\n"), ErrCubeSize},
		"infinity":        {[]byte("LUT_3D_SIZE 2\n0 0 1e400\n"), ErrCubeNumber},
	}
	for name, c := range cases {
		if _, err := ParseCube(c.raw); !errors.Is(err, c.want) {
			t.Errorf("%s: got %v, want %v", name, err, c.want)
		}
	}
}
