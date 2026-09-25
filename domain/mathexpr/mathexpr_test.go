package mathexpr

import (
	"errors"
	"strings"
	"testing"
)

func TestEvaluate(t *testing.T) {
	cases := []struct {
		expr string
		want float64
	}{
		{"1 + 2 * 3", 7},
		{"(1 + 2) * 3", 9},
		{"10 / 4", 2.5},
		{"10 % 4", 2},
		{"-3 + 5", 2},
		{"--3", 3},
		// Right-associative like every calculator the user has used: 2^(3^2), not (2^3)^2.
		{"2 ^ 3 ^ 2", 512},
		{"-2 ^ 2", -4},
		{"1.5e3 / 3", 500},
		{"abs(-4.5)", 4.5},
		{"sqrt(81)", 9},
		{"round(2.345, 2)", 2.35},
		{"round(2.5)", 3},
		{"min(4, 2, 9)", 2},
		{"max(4, 2, 9)", 9},
		{"sum(1, 2, 3, 4)", 10},
		{"avg(2, 4, 9)", 5},
		{"median(9, 1, 5)", 5},
		{"median(1, 2, 3, 10)", 2.5},
		{"pct(25, 200)", 12.5},
		// The question managers ask most: "how much did it change?"
		{"pct_change(80, 100)", 25},
		{"pct_change(-50, -25)", 50},
		{"round(pct_change(1702, 1895), 1)", 11.3},
	}
	for _, tc := range cases {
		t.Run(tc.expr, func(t *testing.T) {
			got, err := Evaluate(tc.expr)
			if err != nil {
				t.Fatalf("Evaluate(%q) error %v", tc.expr, err)
			}
			if got != tc.want {
				t.Fatalf("Evaluate(%q) = %v, want %v", tc.expr, got, tc.want)
			}
		})
	}
}

func TestEvaluateRefusesWhatHasNoHonestNumber(t *testing.T) {
	cases := map[string]string{
		"division by zero":           "1 / 0",
		"modulo by zero":             "1 % 0",
		"percent of nothing":         "pct(3, 0)",
		"change from zero":           "pct_change(0, 10)",
		"root of a negative":         "sqrt(-1)",
		"overflow":                   "10 ^ 400",
		"average of nothing":         "avg()",
		"unknown function":           "exec(1)",
		"bare identifier":            "revenue * 2",
		"wrong arity":                "abs(1, 2)",
		"too many round digits":      "round(1, 99)",
		"dangling operator":          "1 +",
		"unbalanced parenthesis":     "(1 + 2",
		"trailing garbage":           "1 2",
		"empty":                      "   ",
		"letters in a number":        "12abc",
		"thousands separator commas": "1,000 + 1",
	}
	for name, expr := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Evaluate(expr); !errors.Is(err, ErrInvalidExpression) {
				t.Fatalf("Evaluate(%q) err = %v, want ErrInvalidExpression", expr, err)
			}
		})
	}
}

func TestEvaluateBoundsTheWorkItWillDo(t *testing.T) {
	long := strings.Repeat("1+", MaxExpressionLength) + "1"
	if _, err := Evaluate(long); !errors.Is(err, ErrInvalidExpression) {
		t.Fatalf("an over-long expression must be refused, got %v", err)
	}
	deep := strings.Repeat("(", MaxDepth+1) + "1" + strings.Repeat(")", MaxDepth+1)
	if _, err := Evaluate(deep); !errors.Is(err, ErrInvalidExpression) {
		t.Fatalf("an over-deep expression must be refused, got %v", err)
	}
	fine := strings.Repeat("(", MaxDepth-1) + "1" + strings.Repeat(")", MaxDepth-1)
	if _, err := Evaluate(fine); err != nil {
		t.Fatalf("nesting within the limit must work, got %v", err)
	}
}
