package mathexpr

import (
	"errors"
	"math"
	"testing"
)

func TestEvaluateCoversDesignGeometry(t *testing.T) {
	cases := []struct {
		expr string
		want float64
	}{
		{"pi", math.Pi},
		{"round(cos(rad(60)), 6)", 0.5},
		{"round(sin(pi / 6), 6)", 0.5},
		{"round(tan(rad(45)), 6)", 1},
		{"round(deg(atan2(1, 1)), 6)", 45},
		{"round(deg(asin(0.5)), 6)", 30},
		{"round(deg(acos(0.5)), 6)", 60},
		{"round(deg(atan(1)), 6)", 45},
		{"hypot(3, 4)", 5},
		{"floor(2.7)", 2},
		{"ceil(2.1)", 3},
		{"pow(2, 10)", 1024},
		{"lerp(100, 200, 0.25)", 125},
		{"clamp(1.4, 0, 1)", 1},
		{"round(ln(e), 6)", 1},
		{"log10(1000)", 3},
		{"round(exp(0), 6)", 1},
	}
	for _, tc := range cases {
		t.Run(tc.expr, func(t *testing.T) {
			got, err := Evaluate(tc.expr)
			if err != nil {
				t.Fatalf("Evaluate(%q) error %v", tc.expr, err)
			}
			if math.Abs(got-tc.want) > 1e-12 {
				t.Fatalf("Evaluate(%q) = %v, want %v", tc.expr, got, tc.want)
			}
		})
	}
}

func TestEvaluateRefusesGeometryWithNoAnswer(t *testing.T) {
	for _, expr := range []string{"asin(2)", "acos(-1.5)", "ln(0)", "log10(-1)", "clamp(1, 2, 0)", "pi(1)"} {
		if _, err := Evaluate(expr); !errors.Is(err, ErrInvalidExpression) {
			t.Fatalf("Evaluate(%q) err = %v, want ErrInvalidExpression", expr, err)
		}
	}
}

func TestStepsNameValuesThatLaterStepsReuse(t *testing.T) {
	steps, err := EvaluateSteps([]string{
		"w = 640",
		"angle = rad(-35)",
		"tip_x = round(w * 0.5 + 180 * cos(angle), 2)",
		"tip_x / w",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 4 || steps[0].Name != "w" || steps[0].Value != 640 || steps[3].Name != "" {
		t.Fatalf("steps = %+v", steps)
	}
	if steps[2].Value != 467.45 {
		t.Fatalf("tip_x = %v, want 467.45", steps[2].Value)
	}
}

func TestStepsRefuseUnknownNamesAndShadowing(t *testing.T) {
	cases := map[string][]string{
		"name used before it is set": {"a = b + 1", "b = 2"},
		"shadowing a function":       {"sin = 1"},
		"shadowing a constant":       {"pi = 3"},
		"setting a name twice":       {"a = 1", "a = 2"},
		"empty step":                 {"a = 1", "  "},
		"no steps":                   {},
		"malformed name":             {"2a = 1"},
	}
	for name, lines := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := EvaluateSteps(lines); !errors.Is(err, ErrInvalidExpression) {
				t.Fatalf("EvaluateSteps(%q) err = %v, want ErrInvalidExpression", lines, err)
			}
		})
	}
}

func TestAFailingStepSaysWhichOne(t *testing.T) {
	_, err := EvaluateSteps([]string{"a = 1", "a / 0"})
	var failed *StepError
	if !errors.As(err, &failed) || failed.Index != 1 {
		t.Fatalf("err = %v, want the second step named", err)
	}
}
