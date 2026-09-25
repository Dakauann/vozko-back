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

func Evaluate(expr string) (float64, error) {
	if len(expr) > MaxExpressionLength {
		return 0, invalid("expression longer than %d characters", MaxExpressionLength)
	}
	if strings.TrimSpace(expr) == "" {
		return 0, invalid("empty expression")
	}
	p := &parser{src: expr}
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
		return 0, invalid("unknown name %q (only numbers and functions %s are allowed)", name, functionNames())
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
