package emaildesign

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"math/big"
	"regexp"
	"strconv"
	"strings"
)

type OperationKind string

const (
	OpAdd       OperationKind = "add"
	OpSet       OperationKind = "set"
	OpMove      OperationKind = "move"
	OpRemove    OperationKind = "remove"
	OpDuplicate OperationKind = "duplicate"
	OpSettings  OperationKind = "settings"
)

func OperationKinds() []OperationKind {
	return []OperationKind{OpAdd, OpSet, OpMove, OpRemove, OpDuplicate, OpSettings}
}

const refPrefix = "@"

var refPattern = regexp.MustCompile(`^[a-z0-9_-]{1,32}$`)

type Placement struct {
	After  string `json:"after,omitempty"`
	Before string `json:"before,omitempty"`
	Into   string `json:"into,omitempty"`
	Column int    `json:"column,omitempty"`
}

type Operation struct {
	Op     OperationKind              `json:"op"`
	Ref    string                     `json:"ref,omitempty"`
	ID     string                     `json:"id,omitempty"`
	Kind   BlockKind                  `json:"-"`
	Fields map[string]json.RawMessage `json:"fields,omitempty"`
	At     *Placement                 `json:"at,omitempty"`
}

func (o *Operation) UnmarshalJSON(raw []byte) error {
	type wire struct {
		Op     OperationKind              `json:"op"`
		Ref    string                     `json:"ref,omitempty"`
		ID     string                     `json:"id,omitempty"`
		Kind   string                     `json:"kind,omitempty"`
		Fields map[string]json.RawMessage `json:"fields,omitempty"`
		At     *Placement                 `json:"at,omitempty"`
	}
	var w wire
	if err := json.Unmarshal(raw, &w); err != nil {
		return err
	}
	*o = Operation{Op: w.Op, Ref: w.Ref, ID: w.ID, Kind: BlockKind(w.Kind), Fields: w.Fields, At: w.At}
	return nil
}

func (o Operation) MarshalJSON() ([]byte, error) {
	type wire struct {
		Op     OperationKind              `json:"op"`
		Ref    string                     `json:"ref,omitempty"`
		ID     string                     `json:"id,omitempty"`
		Kind   string                     `json:"kind,omitempty"`
		Fields map[string]json.RawMessage `json:"fields,omitempty"`
		At     *Placement                 `json:"at,omitempty"`
	}
	return json.Marshal(wire{Op: o.Op, Ref: o.Ref, ID: o.ID, Kind: string(o.Kind), Fields: o.Fields, At: o.At})
}

type OperationError struct {
	Index int
	Code  string
	Field string
}

func (e *OperationError) Error() string {
	text := "emaildesign: operation " + strconv.Itoa(e.Index+1) + ": " + e.Code
	if e.Field != "" {
		text += " (" + e.Field + ")"
	}
	return text
}

type IDSource func(kind BlockKind, taken map[string]bool) string

const (
	idAlphabet     = "abcdefghijklmnopqrstuvwxyz0123456789"
	idSuffixLength = 6
)

func RandomIDs() IDSource {
	return func(kind BlockKind, taken map[string]bool) string {
		for {
			suffix := make([]byte, idSuffixLength)
			for i := range suffix {
				n, err := rand.Int(rand.Reader, big.NewInt(int64(len(idAlphabet))))
				if err != nil {
					panic(err)
				}
				suffix[i] = idAlphabet[n.Int64()]
			}
			if id := string(kind) + "-" + string(suffix); !taken[id] {
				return id
			}
		}
	}
}

type Step struct {
	Op       OperationKind
	Focus    string
	Document Document
}

type Plan struct {
	Steps   []Step
	Refs    map[string]string
	Created []string
}

func (p Plan) Final(base Document) Document {
	if len(p.Steps) == 0 {
		return base
	}
	return p.Steps[len(p.Steps)-1].Document
}

type container struct {
	columnsID string
	column    int
}

func (c container) root() bool { return c.columnsID == "" }

type location struct {
	block Block
	at    container
	index int
}

type editor struct {
	doc   Document
	ids   IDSource
	refs  map[string]string
	order []string
}

type opFailure struct{ code, field string }

func (f opFailure) Error() string { return f.code }

func fail(code string) error { return opFailure{code: code} }

func Apply(base Document, ops []Operation, ids IDSource) (Plan, error) {
	e := &editor{doc: cloneDocument(base), ids: ids, refs: map[string]string{}}
	plan := Plan{Refs: e.refs}
	for i, o := range ops {
		focus, err := e.apply(o)
		if err != nil {
			return Plan{}, asOperationError(i, err)
		}
		plan.Steps = append(plan.Steps, Step{Op: o.Op, Focus: focus, Document: cloneDocument(e.doc)})
	}
	plan.Created = e.order
	return plan, nil
}

func asOperationError(index int, err error) error {
	var failure opFailure
	if errors.As(err, &failure) {
		return &OperationError{Index: index, Code: failure.code, Field: failure.field}
	}
	var field *FieldError
	if errors.As(err, &field) {
		return &OperationError{Index: index, Code: field.Code, Field: field.Field}
	}
	if errors.Is(err, ErrUnknownBlockKind) {
		return &OperationError{Index: index, Code: "kind_unknown"}
	}
	return &OperationError{Index: index, Code: "operation_failed"}
}

func cloneDocument(doc Document) Document {
	raw, err := json.Marshal(doc)
	if err != nil {
		panic(err)
	}
	var out Document
	if err := json.Unmarshal(raw, &out); err != nil {
		panic(err)
	}
	if out.Blocks == nil {
		out.Blocks = []Block{}
	}
	return out
}

func (e *editor) apply(o Operation) (string, error) {
	switch o.Op {
	case OpAdd:
		return e.add(o)
	case OpSet:
		return e.set(o)
	case OpMove:
		return e.move(o)
	case OpRemove:
		return "", e.remove(o)
	case OpDuplicate:
		return e.duplicate(o)
	case OpSettings:
		return "", e.settings(o)
	}
	return "", fail("operation_unknown")
}

func (e *editor) resolve(id string) (string, error) {
	id = strings.TrimSpace(id)
	if name, ok := strings.CutPrefix(id, refPrefix); ok {
		resolved, known := e.refs[name]
		if !known {
			return "", fail("ref_unknown")
		}
		return resolved, nil
	}
	return id, nil
}

func (e *editor) target(id string) (location, error) {
	if strings.TrimSpace(id) == "" {
		return location{}, fail("id_required")
	}
	resolved, err := e.resolve(id)
	if err != nil {
		return location{}, err
	}
	found, ok := e.find(resolved)
	if !ok {
		return location{}, fail("block_not_found")
	}
	return found, nil
}

func (e *editor) find(id string) (location, bool) {
	for i, block := range e.doc.Blocks {
		if block.ID == id {
			return location{block: block, at: container{}, index: i}, true
		}
	}
	for _, parent := range e.doc.Blocks {
		if parent.Kind != KindColumns || parent.Columns == nil {
			continue
		}
		for c, column := range parent.Columns.Columns {
			for i, child := range column.Blocks {
				if child.ID == id {
					return location{block: child, at: container{columnsID: parent.ID, column: c}, index: i}, true
				}
			}
		}
	}
	return location{}, false
}

func (e *editor) claimRef(ref, id string) error {
	if ref == "" {
		return nil
	}
	if !refPattern.MatchString(ref) {
		return fail("ref_invalid")
	}
	if _, taken := e.refs[ref]; taken {
		return fail("ref_duplicate")
	}
	e.refs[ref] = id
	return nil
}

func (e *editor) taken() map[string]bool {
	out := map[string]bool{}
	var walk func(blocks []Block)
	walk = func(blocks []Block) {
		for _, b := range blocks {
			out[b.ID] = true
			if b.Kind == KindColumns && b.Columns != nil {
				for _, column := range b.Columns.Columns {
					walk(column.Blocks)
				}
			}
		}
	}
	walk(e.doc.Blocks)
	return out
}

func (e *editor) count() int {
	return len(e.taken())
}

func (e *editor) footerIndex() int {
	for i, b := range e.doc.Blocks {
		if b.Kind == KindFooter {
			return i
		}
	}
	return -1
}

func (e *editor) rootLimit() int {
	if footer := e.footerIndex(); footer >= 0 {
		return footer
	}
	return len(e.doc.Blocks)
}

func (e *editor) columnsBlock(id string) (int, bool) {
	for i, b := range e.doc.Blocks {
		if b.ID == id {
			return i, b.Kind == KindColumns && b.Columns != nil
		}
	}
	return -1, false
}

func (e *editor) list(at container) []Block {
	if at.root() {
		return e.doc.Blocks
	}
	i, _ := e.columnsBlock(at.columnsID)
	return e.doc.Blocks[i].Columns.Columns[at.column].Blocks
}

func (e *editor) setList(at container, blocks []Block) {
	if at.root() {
		e.doc.Blocks = blocks
		return
	}
	i, _ := e.columnsBlock(at.columnsID)
	e.doc.Blocks[i].Columns.Columns[at.column].Blocks = blocks
}

func (e *editor) placement(p *Placement) (container, int, error) {
	if p == nil {
		return container{}, e.rootLimit(), nil
	}
	anchors := 0
	for _, v := range []string{p.After, p.Before, p.Into} {
		if strings.TrimSpace(v) != "" {
			anchors++
		}
	}
	if anchors != 1 || (p.Column != 0 && strings.TrimSpace(p.Into) == "") {
		return container{}, 0, fail("position_invalid")
	}
	if strings.TrimSpace(p.Into) != "" {
		id, err := e.resolve(p.Into)
		if err != nil {
			return container{}, 0, err
		}
		i, ok := e.columnsBlock(id)
		switch {
		case i < 0:
			if _, nested := e.find(id); nested {
				return container{}, 0, fail("position_invalid")
			}
			return container{}, 0, fail("block_not_found")
		case !ok:
			return container{}, 0, fail("position_invalid")
		case p.Column < 0 || p.Column >= len(e.doc.Blocks[i].Columns.Columns):
			return container{}, 0, fail("column_invalid")
		}
		at := container{columnsID: id, column: p.Column}
		return at, len(e.list(at)), nil
	}
	anchor := p.After
	shift := 1
	if strings.TrimSpace(p.Before) != "" {
		anchor, shift = p.Before, 0
	}
	found, err := e.target(anchor)
	if err != nil {
		return container{}, 0, err
	}
	return found.at, found.index + shift, nil
}

func childKind(kind BlockKind) bool {
	return kind != KindColumns && kind != KindFooter
}

func (e *editor) insert(block Block, at container, index int) error {
	if block.Kind == KindFooter {
		if !at.root() || e.footerIndex() >= 0 {
			return fail("footer_exists")
		}
		e.doc.Blocks = append(e.doc.Blocks, block)
		return nil
	}
	if !at.root() && !childKind(block.Kind) {
		return fail("not_allowed_in_columns")
	}
	list := e.list(at)
	limit := len(list)
	if at.root() {
		limit = e.rootLimit()
	}
	index = min(max(index, 0), limit)
	next := make([]Block, 0, len(list)+1)
	next = append(next, list[:index]...)
	next = append(next, block)
	next = append(next, list[index:]...)
	e.setList(at, next)
	return nil
}

func (e *editor) detach(found location) {
	list := e.list(found.at)
	next := make([]Block, 0, len(list)-1)
	next = append(next, list[:found.index]...)
	next = append(next, list[found.index+1:]...)
	e.setList(found.at, next)
}

func (e *editor) add(o Operation) (string, error) {
	if o.Kind == "" {
		return "", fail("kind_required")
	}
	if !o.Kind.Known() {
		return "", fail("kind_unknown")
	}
	if e.count()+1 > MaxBlocks {
		return "", fail("too_many_blocks")
	}
	at, index, err := e.placement(o.At)
	if err != nil {
		return "", err
	}
	if o.Kind == KindFooter && e.footerIndex() >= 0 {
		return "", fail("footer_exists")
	}
	if !at.root() && !childKind(o.Kind) {
		return "", fail("not_allowed_in_columns")
	}
	block, err := NewBlock(e.ids(o.Kind, e.taken()), o.Kind, o.Fields)
	if err != nil {
		return "", err
	}
	if err := e.claimRef(o.Ref, block.ID); err != nil {
		return "", err
	}
	if err := e.insert(block, at, index); err != nil {
		return "", err
	}
	e.order = append(e.order, block.ID)
	return block.ID, nil
}

func (e *editor) set(o Operation) (string, error) {
	found, err := e.target(o.ID)
	if err != nil {
		return "", err
	}
	if len(o.Fields) == 0 {
		return "", fail("fields_required")
	}
	patched, err := PatchBlock(found.block, o.Fields)
	if err != nil {
		return "", err
	}
	list := e.list(found.at)
	list[found.index] = patched
	e.setList(found.at, list)
	return patched.ID, nil
}

func (e *editor) move(o Operation) (string, error) {
	found, err := e.target(o.ID)
	if err != nil {
		return "", err
	}
	if found.block.Kind == KindFooter {
		return "", fail("footer_locked")
	}
	at, index, err := e.placement(o.At)
	if err != nil {
		return "", err
	}
	if !at.root() && !childKind(found.block.Kind) {
		return "", fail("not_allowed_in_columns")
	}
	if at == found.at && found.index < index {
		index--
	}
	e.detach(found)
	if err := e.insert(found.block, at, index); err != nil {
		return "", err
	}
	return found.block.ID, nil
}

func (e *editor) remove(o Operation) error {
	found, err := e.target(o.ID)
	if err != nil {
		return err
	}
	if found.block.Kind == KindFooter {
		return fail("footer_locked")
	}
	e.detach(found)
	return nil
}

func (e *editor) duplicate(o Operation) (string, error) {
	found, err := e.target(o.ID)
	if err != nil {
		return "", err
	}
	if found.block.Kind == KindFooter {
		return "", fail("footer_locked")
	}
	copied := cloneBlock(found.block)
	size := 1
	if copied.Kind == KindColumns {
		for _, column := range copied.Columns.Columns {
			size += len(column.Blocks)
		}
	}
	if e.count()+size > MaxBlocks {
		return "", fail("too_many_blocks")
	}
	taken := e.taken()
	e.freshIDs(&copied, taken)
	if err := e.claimRef(o.Ref, copied.ID); err != nil {
		return "", err
	}
	if err := e.insert(copied, found.at, found.index+1); err != nil {
		return "", err
	}
	e.order = append(e.order, copied.ID)
	return copied.ID, nil
}

func (e *editor) freshIDs(block *Block, taken map[string]bool) {
	block.ID = e.ids(block.Kind, taken)
	taken[block.ID] = true
	if block.Kind != KindColumns || block.Columns == nil {
		return
	}
	for c := range block.Columns.Columns {
		for i := range block.Columns.Columns[c].Blocks {
			e.freshIDs(&block.Columns.Columns[c].Blocks[i], taken)
		}
	}
}

func cloneBlock(b Block) Block {
	raw, err := json.Marshal(b)
	if err != nil {
		panic(err)
	}
	var out Block
	if err := json.Unmarshal(raw, &out); err != nil {
		panic(err)
	}
	return out
}

func (e *editor) settings(o Operation) error {
	if len(o.Fields) == 0 {
		return fail("fields_required")
	}
	next, err := PatchSettings(e.doc.Settings, o.Fields)
	if err != nil {
		return err
	}
	e.doc.Settings = next
	return nil
}
