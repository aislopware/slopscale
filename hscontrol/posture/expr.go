// Package posture evaluates device posture expressions of the form
// Tailscale's policy uses ("node:os IN ['macos', 'linux']",
// "node:tsVersion >= '1.40'", "custom:oncall == true") against a node's
// attribute map, and the schedules a posture can carry.
package posture

import (
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

// Op is a comparison operator.
type Op string

// The operators an expression accepts.
const (
	OpEqual        Op = "=="
	OpNotEqual     Op = "!="
	OpLess         Op = "<"
	OpLessEqual    Op = "<="
	OpGreater      Op = ">"
	OpGreaterEqual Op = ">="
	OpIn           Op = "IN"
	OpNotIn        Op = "NOT IN"
	OpIsSet        Op = "IS SET"
	OpNotSet       Op = "NOT SET"
)

// Errors returned by [Parse].
var (
	ErrEmpty         = errors.New("posture expression is empty")
	ErrAttribute     = errors.New("posture expression must start with an attribute such as node:os")
	ErrOperator      = errors.New("posture expression has no operator")
	ErrValue         = errors.New("posture expression has no value")
	ErrTrailing      = errors.New("posture expression has text after the value")
	ErrListExpected  = errors.New("IN and NOT IN need a list such as ['a', 'b'] of values")
	ErrScalarWanted  = errors.New("this operator takes a single value, not a list")
	ErrOrderedValue  = errors.New("<, <=, > and >= compare numbers or version strings")
	ErrUnterminated  = errors.New("unterminated string in posture expression")
	ErrUnknownPrefix = errors.New(
		"attribute prefix must be one of node, custom, ip or an integration's: " +
			"falcon, sentinelOne, intune, jamfPro, kandji, kolide",
	)
)

// KnownPrefixes are the attribute namespaces an expression may read:
// node: from what the client reports, custom: set by an operator, ip:
// from where the node connects, and one per posture integration
// (see types.PostureProvider).
var KnownPrefixes = []string{"node", "custom", "ip", "falcon", "sentinelOne", "intune", "jamfPro", "kandji", "kolide"}

// Value is a literal in an expression: a string, a number, a boolean,
// or a list of those after IN.
type Value struct {
	Kind ValueKind
	Str  string
	Num  float64
	Bool bool
	List []Value
}

// ValueKind tells which field of a [Value] is set.
type ValueKind int

// The kinds of literal.
const (
	KindString ValueKind = iota + 1
	KindNumber
	KindBool
	KindList
)

// Expr is one parsed expression.
type Expr struct {
	Attr  string
	Op    Op
	Value Value
	src   string
}

// String returns the expression as written.
func (e Expr) String() string { return e.src }

// UsesSourceAddress reports whether the expression reads an ip:
// attribute, which comes from where the node connects from rather than
// from what it reports.
func (e Expr) UsesSourceAddress() bool {
	return strings.HasPrefix(e.Attr, "ip:")
}

// ParseAll parses every expression and reports the first bad one with
// its text.
func ParseAll(exprs []string) ([]Expr, error) {
	out := make([]Expr, 0, len(exprs))

	for _, s := range exprs {
		e, err := Parse(s)
		if err != nil {
			return nil, fmt.Errorf("%q: %w", s, err)
		}

		out = append(out, e)
	}

	return out, nil
}

// Parse parses one expression.
func Parse(s string) (Expr, error) {
	p := parser{s: strings.TrimSpace(s)}
	if p.s == "" {
		return Expr{}, ErrEmpty
	}

	attr, err := p.attribute()
	if err != nil {
		return Expr{}, err
	}

	op, err := p.operator()
	if err != nil {
		return Expr{}, err
	}

	e := Expr{Attr: attr, Op: op, src: p.s}

	switch op {
	case OpIsSet, OpNotSet:
		if !p.done() {
			return Expr{}, ErrTrailing
		}

		return e, nil
	case OpIn, OpNotIn:
		v, err := p.value()
		if err != nil {
			return Expr{}, err
		}

		if v.Kind != KindList {
			return Expr{}, ErrListExpected
		}

		e.Value = v
	case OpEqual, OpNotEqual, OpLess, OpLessEqual, OpGreater, OpGreaterEqual:
		v, err := p.value()
		if err != nil {
			return Expr{}, err
		}

		if v.Kind == KindList {
			return Expr{}, ErrScalarWanted
		}

		if v.Kind == KindBool && op != OpEqual && op != OpNotEqual {
			return Expr{}, ErrOrderedValue
		}

		e.Value = v
	}

	if !p.done() {
		return Expr{}, ErrTrailing
	}

	return e, nil
}

type parser struct {
	s string
	i int
}

func (p *parser) skipSpace() {
	for p.i < len(p.s) && unicode.IsSpace(rune(p.s[p.i])) {
		p.i++
	}
}

func (p *parser) done() bool {
	p.skipSpace()

	return p.i >= len(p.s)
}

func (p *parser) attribute() (string, error) {
	p.skipSpace()

	start := p.i
	for p.i < len(p.s) && isAttrByte(p.s[p.i]) {
		p.i++
	}

	attr := p.s[start:p.i]

	prefix, _, ok := strings.Cut(attr, ":")
	if !ok || prefix == "" || prefix == attr {
		return "", ErrAttribute
	}

	if !slices.Contains(KnownPrefixes, prefix) {
		return "", fmt.Errorf("%w, got %q", ErrUnknownPrefix, prefix)
	}

	return attr, nil
}

func isAttrByte(b byte) bool {
	return b == ':' || b == '_' || b == '-' || b == '.' ||
		(b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// operators lists the operators longest first so "<=" wins over "<".
var operators = []Op{
	OpNotIn,
	OpIsSet,
	OpNotSet,
	OpLessEqual,
	OpGreaterEqual,
	OpEqual,
	OpNotEqual,
	OpLess,
	OpGreater,
	OpIn,
}

func (p *parser) operator() (Op, error) {
	p.skipSpace()

	rest := p.s[p.i:]

	for _, op := range operators {
		if !hasOpPrefix(rest, string(op)) {
			continue
		}

		p.i += len(op)

		return op, nil
	}

	return "", ErrOperator
}

// hasOpPrefix matches an operator case-insensitively for the word
// operators and requires a word operator to end at a word boundary.
func hasOpPrefix(s, op string) bool {
	if len(s) < len(op) || !strings.EqualFold(s[:len(op)], op) {
		return false
	}

	if unicode.IsLetter(rune(op[0])) && len(s) > len(op) && isAttrByte(s[len(op)]) {
		return false
	}

	return true
}

func (p *parser) value() (Value, error) {
	p.skipSpace()

	if p.i >= len(p.s) {
		return Value{}, ErrValue
	}

	switch c := p.s[p.i]; c {
	case '[':
		return p.list()
	case '\'', '"':
		return p.quoted()
	default:
		return p.bare()
	}
}

func (p *parser) list() (Value, error) {
	p.i++ // [

	v := Value{Kind: KindList, List: []Value{}}

	for {
		p.skipSpace()

		if p.i >= len(p.s) {
			return Value{}, ErrValue
		}

		if p.s[p.i] == ']' {
			p.i++

			return v, nil
		}

		if len(v.List) > 0 {
			if p.s[p.i] != ',' {
				return Value{}, ErrValue
			}

			p.i++
			p.skipSpace()
		}

		item, err := p.value()
		if err != nil {
			return Value{}, err
		}

		if item.Kind == KindList {
			return Value{}, ErrScalarWanted
		}

		v.List = append(v.List, item)
	}
}

func (p *parser) quoted() (Value, error) {
	quote := p.s[p.i]
	p.i++

	var b strings.Builder

	for p.i < len(p.s) {
		c := p.s[p.i]
		p.i++

		switch {
		case c == '\\' && p.i < len(p.s):
			b.WriteByte(p.s[p.i])
			p.i++
		case c == quote:
			return Value{Kind: KindString, Str: b.String()}, nil
		default:
			b.WriteByte(c)
		}
	}

	return Value{}, ErrUnterminated
}

func (p *parser) bare() (Value, error) {
	start := p.i
	for p.i < len(p.s) && p.s[p.i] != ',' && p.s[p.i] != ']' && !unicode.IsSpace(rune(p.s[p.i])) {
		p.i++
	}

	word := p.s[start:p.i]

	switch strings.ToLower(word) {
	case "":
		return Value{}, ErrValue
	case "true":
		return Value{Kind: KindBool, Bool: true}, nil
	case "false":
		return Value{Kind: KindBool, Bool: false}, nil
	}

	n, err := strconv.ParseFloat(word, 64)
	if err != nil {
		return Value{}, fmt.Errorf("%w: %q is not a number, true, false or a quoted string", ErrValue, word)
	}

	return Value{Kind: KindNumber, Num: n}, nil
}

// Eval reports whether the node's attributes satisfy the expression. A
// missing attribute satisfies only NOT SET. A list-valued attribute
// (serial numbers) matches ==, IN and the ordered operators when any
// element does, and != and NOT IN when none does.
func (e Expr) Eval(attrs map[string]any) bool {
	raw, ok := attrs[e.Attr]
	if !ok || raw == nil {
		return e.Op == OpNotSet
	}

	switch e.Op {
	case OpIsSet:
		return true
	case OpNotSet:
		return false
	case OpNotEqual:
		return !anyElement(raw, func(v any) bool { return equal(v, e.Value) })
	case OpNotIn:
		return !anyElement(raw, func(v any) bool { return inList(v, e.Value.List) })
	case OpEqual:
		return anyElement(raw, func(v any) bool { return equal(v, e.Value) })
	case OpIn:
		return anyElement(raw, func(v any) bool { return inList(v, e.Value.List) })
	case OpLess, OpLessEqual, OpGreater, OpGreaterEqual:
		return anyElement(raw, func(v any) bool { return ordered(v, e.Op, e.Value) })
	}

	return false
}

func anyElement(raw any, f func(any) bool) bool {
	if list, ok := raw.([]string); ok {
		for _, s := range list {
			if f(s) {
				return true
			}
		}

		return false
	}

	return f(raw)
}

func inList(v any, list []Value) bool {
	for _, item := range list {
		if equal(v, item) {
			return true
		}
	}

	return false
}

// equal compares an attribute value with a literal. A string literal in
// CIDR form matches an address inside it, which is how ip:address is
// checked against ranges.
func equal(v any, lit Value) bool {
	switch a := v.(type) {
	case string:
		if lit.Kind != KindString {
			return false
		}

		if a == lit.Str {
			return true
		}

		return addrInPrefix(a, lit.Str)
	case float64:
		return lit.Kind == KindNumber && a == lit.Num
	case int:
		return lit.Kind == KindNumber && float64(a) == lit.Num
	case int64:
		return lit.Kind == KindNumber && float64(a) == lit.Num
	case bool:
		return lit.Kind == KindBool && a == lit.Bool
	}

	return false
}

func addrInPrefix(addr, cidr string) bool {
	if !strings.Contains(cidr, "/") {
		return false
	}

	prefix, err := netip.ParsePrefix(cidr)
	if err != nil {
		return false
	}

	a, err := netip.ParseAddr(addr)
	if err != nil {
		return false
	}

	return prefix.Contains(a.Unmap())
}

func ordered(v any, op Op, lit Value) bool {
	var c int

	switch a := v.(type) {
	case float64:
		if lit.Kind != KindNumber {
			return false
		}

		c = compareFloat(a, lit.Num)
	case int:
		if lit.Kind != KindNumber {
			return false
		}

		c = compareFloat(float64(a), lit.Num)
	case string:
		if lit.Kind != KindString {
			return false
		}

		c = CompareVersions(a, lit.Str)
	default:
		return false
	}

	switch op {
	case OpLess:
		return c < 0
	case OpLessEqual:
		return c <= 0
	case OpGreater:
		return c > 0
	case OpGreaterEqual:
		return c >= 0
	case OpEqual, OpNotEqual, OpIn, OpNotIn, OpIsSet, OpNotSet:
		return false
	}

	return false
}

func compareFloat(a, b float64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

// CompareVersions orders two version strings segment by segment:
// numeric segments by value, other segments as text, and a missing
// segment as lower than a present one, so "1.40" < "1.40.1" and
// "1.86.2" > "1.9".
func CompareVersions(a, b string) int {
	as := versionSegments(a)
	bs := versionSegments(b)

	for i := range max(len(as), len(bs)) {
		if i >= len(as) {
			return -1
		}

		if i >= len(bs) {
			return 1
		}

		if c := compareSegment(as[i], bs[i]); c != 0 {
			return c
		}
	}

	return 0
}

func versionSegments(v string) []string {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")

	return strings.FieldsFunc(v, func(r rune) bool { return r == '.' || r == '-' })
}

func compareSegment(a, b string) int {
	an, aerr := strconv.ParseUint(a, 10, 64)
	bn, berr := strconv.ParseUint(b, 10, 64)

	switch {
	case aerr == nil && berr == nil:
		return compareFloat(float64(an), float64(bn))
	case aerr == nil:
		// A number sorts above text ("1.40.0" > "1.40.rc1").
		return 1
	case berr == nil:
		return -1
	default:
		return strings.Compare(a, b)
	}
}

// EvalAll reports whether every expression holds.
func EvalAll(exprs []Expr, attrs map[string]any) bool {
	for _, e := range exprs {
		if !e.Eval(attrs) {
			return false
		}
	}

	return true
}

// UsesSourceAddress reports whether any expression reads an ip:
// attribute.
func UsesSourceAddress(exprs []Expr) bool {
	return slices.ContainsFunc(exprs, Expr.UsesSourceAddress)
}
