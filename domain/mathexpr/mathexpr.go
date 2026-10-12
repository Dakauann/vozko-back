package mathexpr

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

const (
	MaxExpressionLength = 2000
	MaxDepth            = 32
	maxRoundDigits      = 10
)

var ErrInvalidExpression = errors.New("mathexpr: invalid expression")

type Step struct {
	Name       string
	Expression string
	Value      float64
}

type StepError struct {
	Index int
	Err   error
}

func (e *StepError) Error() string { return fmt.Sprintf("step %d: %v", e.Index+1, e.Err) }

func (e *StepError) Unwrap() error { return e.Err }

func Evaluate(expr string) (float64, error) {
	return evaluate(expr, nil)
}

func EvaluateSteps(lines []string) ([]Step, error) {
	if len(lines) == 0 {
		return nil, invalid("no steps")
	}
	names := map[string]float64{}
	steps := make([]Step, 0, len(lines))
	for i, line := range lines {
		step, err := evaluateStep(line, names)
		if err != nil {
			return nil, &StepError{Index: i, Err: err}
		}
		if step.Name != "" {
			names[step.Name] = step.Value
		}
		steps = append(steps, step)
	}
	return steps, nil
}

func evaluateStep(line string, names map[string]float64) (Step, error) {
	name, expr := "", line
	if at := strings.Index(line, "="); at >= 0 {
		name, expr = strings.ToLower(strings.TrimSpace(line[:at])), line[at+1:]
		if err := assignable(name, names); err != nil {
			return Step{}, err
		}
	}
	v, err := evaluate(expr, names)
	if err != nil {
		return Step{}, err
	}
	return Step{Name: name, Expression: strings.TrimSpace(expr), Value: v}, nil
}

func assignable(name string, names map[string]float64) error {
	if !isIdentifier(name) {
		return invalid("%q is not a valid name (letters, digits and _, starting with a letter)", name)
	}
	if _, taken := functions[name]; taken {
		return invalid("%q is a function and cannot be a name", name)
	}
	if _, taken := constants[name]; taken {
		return invalid("%q is a constant and cannot be a name", name)
	}
	if _, taken := names[name]; taken {
		return invalid("%q was already set in an earlier step", name)
	}
	return nil
}

func isIdentifier(name string) bool {
	if name == "" || !(unicode.IsLetter(rune(name[0])) || name[0] == '_') {
		return false
	}
	for i := 0; i < len(name); i++ {
		if !(unicode.IsLetter(rune(name[i])) || isDigit(name[i]) || name[i] == '_') {
			return false
		}
	}
	return true
}

func evaluate(expr string, names map[string]float64) (float64, error) {
	if len(expr) > MaxExpressionLength {
		return 0, invalid("expression longer than %d characters", MaxExpressionLength)
	}
	if strings.TrimSpace(expr) == "" {
		return 0, invalid("empty expression")
	}
	p := &parser{src: expr, names: names}
	v, err := p.expression()
	if err != nil {
		return 0, err
	}
	p.skipSpace()
	if p.pos < len(p.src) {
		return 0, invalid("unexpected %q at position %d", p.src[p.pos:], p.pos)
	}
	return finite(v)
}

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidExpression, fmt.Sprintf(format, args...))
}

func finite(v float64) (float64, error) {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, invalid("result is not a finite number")
	}
	return v, nil
}

type parser struct {
	src   string
	pos   int
	depth int
	names map[string]float64
}

func (p *parser) skipSpace() {
	for p.pos < len(p.src) && unicode.IsSpace(rune(p.src[p.pos])) {
		p.pos++
	}
}

func (p *parser) peek() byte {
	p.skipSpace()
	if p.pos >= len(p.src) {
		return 0
	}
	return p.src[p.pos]
}

func (p *parser) enter() error {
	p.depth++
	if p.depth > MaxDepth {
		return invalid("nesting deeper than %d", MaxDepth)
	}
	return nil
}

func (p *parser) leave() { p.depth-- }

func (p *parser) expression() (float64, error) {
	if err := p.enter(); err != nil {
		return 0, err
	}
	defer p.leave()
	left, err := p.term()
	if err != nil {
		return 0, err
	}
	for {
		op := p.peek()
		if op != '+' && op != '-' {
			return left, nil
		}
		p.pos++
		right, err := p.term()
		if err != nil {
			return 0, err
		}
		if op == '+' {
			left += right
		} else {
			left -= right
		}
	}
}

func (p *parser) term() (float64, error) {
	left, err := p.unary()
	if err != nil {
		return 0, err
	}
	for {
		op := p.peek()
		if op != '*' && op != '/' && op != '%' {
			return left, nil
		}
		p.pos++
		right, err := p.unary()
		if err != nil {
			return 0, err
		}
		switch op {
		case '*':
			left *= right
		case '/':
			if right == 0 {
				return 0, invalid("division by zero")
			}
			left /= right
		case '%':
			if right == 0 {
				return 0, invalid("modulo by zero")
			}
			left = math.Mod(left, right)
		}
	}
}

func (p *parser) unary() (float64, error) {
	switch p.peek() {
	case '-', '+':
		op := p.src[p.pos]
		p.pos++
		if err := p.enter(); err != nil {
			return 0, err
		}
		defer p.leave()
		v, err := p.unary()
		if err != nil {
			return 0, err
		}
		if op == '-' {
			return -v, nil
		}
		return v, nil
	}
	return p.power()
}

func (p *parser) power() (float64, error) {
	base, err := p.primary()
	if err != nil {
		return 0, err
	}
	if p.peek() != '^' {
		return base, nil
	}
	p.pos++
	exponent, err := p.unary()
	if err != nil {
		return 0, err
	}
	return finite(math.Pow(base, exponent))
}

func (p *parser) primary() (float64, error) {
	c := p.peek()
	switch {
	case c == 0:
		return 0, invalid("unexpected end of expression")
	case c == '(':
		p.pos++
		v, err := p.expression()
		if err != nil {
			return 0, err
		}
		if p.peek() != ')' {
			return 0, invalid("missing closing parenthesis")
		}
		p.pos++
		return v, nil
	case c == '.' || (c >= '0' && c <= '9'):
		return p.number()
	case unicode.IsLetter(rune(c)) || c == '_':
		return p.call()
	}
	return 0, invalid("unexpected %q at position %d", string(c), p.pos)
}

func (p *parser) number() (float64, error) {
	start := p.pos
	for p.pos < len(p.src) && (isDigit(p.src[p.pos]) || p.src[p.pos] == '.') {
		p.pos++
	}
	if p.pos < len(p.src) && (p.src[p.pos] == 'e' || p.src[p.pos] == 'E') {
		p.pos++
		if p.pos < len(p.src) && (p.src[p.pos] == '+' || p.src[p.pos] == '-') {
			p.pos++
		}
		for p.pos < len(p.src) && isDigit(p.src[p.pos]) {
			p.pos++
		}
	}
	if p.pos < len(p.src) && (unicode.IsLetter(rune(p.src[p.pos])) || p.src[p.pos] == '_') {
		return 0, invalid("malformed number at position %d", start)
	}
	v, err := strconv.ParseFloat(p.src[start:p.pos], 64)
	if err != nil {
		return 0, invalid("malformed number %q", p.src[start:p.pos])
	}
	return finite(v)
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func (p *parser) call() (float64, error) {
	start := p.pos
	for p.pos < len(p.src) && (unicode.IsLetter(rune(p.src[p.pos])) || isDigit(p.src[p.pos]) || p.src[p.pos] == '_') {
		p.pos++
	}
	name := strings.ToLower(p.src[start:p.pos])
	fn, found := functions[name]
	if !found {
		return p.named(name)
	}
	if p.peek() != '(' {
		return 0, invalid("%s must be called with parentheses", name)
	}
	p.pos++
	args, err := p.arguments()
	if err != nil {
		return 0, err
	}
	if len(args) < fn.minArgs || (fn.maxArgs > 0 && len(args) > fn.maxArgs) {
		return 0, invalid("%s takes %s", name, fn.arity())
	}
	v, err := fn.apply(args)
	if err != nil {
		return 0, err
	}
	return finite(v)
}

func (p *parser) named(name string) (float64, error) {
	if p.peek() == '(' {
		return 0, invalid("unknown function %q (functions: %s)", name, functionNames())
	}
	if v, ok := constants[name]; ok {
		return v, nil
	}
	if v, ok := p.names[name]; ok {
		return v, nil
	}
	return 0, invalid("unknown name %q (only numbers, the constants pi and e, names set in earlier steps and functions %s are allowed)", name, functionNames())
}

func (p *parser) arguments() ([]float64, error) {
	var args []float64
	if p.peek() == ')' {
		p.pos++
		return args, nil
	}
	for {
		v, err := p.expression()
		if err != nil {
			return nil, err
		}
		args = append(args, v)
		switch p.peek() {
		case ',':
			p.pos++
		case ')':
			p.pos++
			return args, nil
		default:
			return nil, invalid("expected , or ) in the argument list")
		}
	}
}

type function struct {
	minArgs int
	maxArgs int
	apply   func([]float64) (float64, error)
}

func (f function) arity() string {
	switch {
	case f.maxArgs == 0:
		return fmt.Sprintf("at least %d argument(s)", f.minArgs)
	case f.minArgs == f.maxArgs:
		return fmt.Sprintf("exactly %d argument(s)", f.minArgs)
	}
	return fmt.Sprintf("%d to %d arguments", f.minArgs, f.maxArgs)
}

var constants = map[string]float64{"pi": math.Pi, "e": math.E}

func unary(apply func(float64) float64) function {
	return function{1, 1, func(a []float64) (float64, error) { return apply(a[0]), nil }}
}

func inUnitRange(name string, apply func(float64) float64) function {
	return function{1, 1, func(a []float64) (float64, error) {
		if a[0] < -1 || a[0] > 1 {
			return 0, invalid("%s takes a value from -1 to 1", name)
		}
		return apply(a[0]), nil
	}}
}

func positive(name string, apply func(float64) float64) function {
	return function{1, 1, func(a []float64) (float64, error) {
		if a[0] <= 0 {
			return 0, invalid("%s takes a positive number", name)
		}
		return apply(a[0]), nil
	}}
}

var functions = map[string]function{
	"abs": {1, 1, func(a []float64) (float64, error) { return math.Abs(a[0]), nil }},
	"sqrt": {1, 1, func(a []float64) (float64, error) {
		if a[0] < 0 {
			return 0, invalid("square root of a negative number")
		}
		return math.Sqrt(a[0]), nil
	}},
	"round": {1, 2, func(a []float64) (float64, error) {
		digits := 0.0
		if len(a) == 2 {
			digits = a[1]
		}
		if digits < 0 || digits > maxRoundDigits || digits != math.Trunc(digits) {
			return 0, invalid("round digits must be a whole number from 0 to %d", maxRoundDigits)
		}
		scale := math.Pow(10, digits)
		return math.Round(a[0]*scale) / scale, nil
	}},
	"min": {1, 0, func(a []float64) (float64, error) {
		out := a[0]
		for _, v := range a[1:] {
			out = math.Min(out, v)
		}
		return out, nil
	}},
	"max": {1, 0, func(a []float64) (float64, error) {
		out := a[0]
		for _, v := range a[1:] {
			out = math.Max(out, v)
		}
		return out, nil
	}},
	"sum": {1, 0, func(a []float64) (float64, error) { return sum(a), nil }},
	"avg": {1, 0, func(a []float64) (float64, error) { return sum(a) / float64(len(a)), nil }},
	"median": {1, 0, func(a []float64) (float64, error) {
		sorted := append([]float64(nil), a...)
		sort.Float64s(sorted)
		mid := len(sorted) / 2
		if len(sorted)%2 == 1 {
			return sorted[mid], nil
		}
		return (sorted[mid-1] + sorted[mid]) / 2, nil
	}},
	"pct": {2, 2, func(a []float64) (float64, error) {
		if a[1] == 0 {
			return 0, invalid("pct of a zero whole")
		}
		return a[0] / a[1] * 100, nil
	}},
	"pct_change": {2, 2, func(a []float64) (float64, error) {
		if a[0] == 0 {
			return 0, invalid("pct_change from zero has no percentage")
		}
		return (a[1] - a[0]) / math.Abs(a[0]) * 100, nil
	}},
	"sin":   unary(math.Sin),
	"cos":   unary(math.Cos),
	"tan":   unary(math.Tan),
	"asin":  inUnitRange("asin", math.Asin),
	"acos":  inUnitRange("acos", math.Acos),
	"atan":  unary(math.Atan),
	"atan2": {2, 2, func(a []float64) (float64, error) { return math.Atan2(a[0], a[1]), nil }},
	"hypot": {2, 2, func(a []float64) (float64, error) { return math.Hypot(a[0], a[1]), nil }},
	"rad":   unary(func(x float64) float64 { return x * math.Pi / 180 }),
	"deg":   unary(func(x float64) float64 { return x * 180 / math.Pi }),
	"floor": unary(math.Floor),
	"ceil":  unary(math.Ceil),
	"pow":   {2, 2, func(a []float64) (float64, error) { return math.Pow(a[0], a[1]), nil }},
	"exp":   unary(math.Exp),
	"ln":    positive("ln", math.Log),
	"log10": positive("log10", math.Log10),
	"lerp":  {3, 3, func(a []float64) (float64, error) { return a[0] + (a[1]-a[0])*a[2], nil }},
	"clamp": {3, 3, func(a []float64) (float64, error) {
		if a[1] > a[2] {
			return 0, invalid("clamp needs the low bound before the high bound")
		}
		return math.Min(math.Max(a[0], a[1]), a[2]), nil
	}},
}

func sum(a []float64) float64 {
	total := 0.0
	for _, v := range a {
		total += v
	}
	return total
}

func functionNames() string {
	names := make([]string, 0, len(functions))
	for name := range functions {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}
