package workflow

import "strings"

type provisionMatch int

const (
	matchExact provisionMatch = iota
	matchOpen
	matchFamily
	matchAnyVariable
)

type provision struct {
	ref     string
	match   provisionMatch
	label   string
	group   VariableGroup
	kind    VariableKind
	typ     VariableType
	example *string
	hidden  bool
	node    int
	edge    func(label string) bool
}

type flowEdge struct {
	from  int
	to    int
	label string
}

type bitset []uint64

func newBitset(size int) bitset { return make(bitset, size/64+1) }

func (b bitset) set(i int)      { b[i/64] |= 1 << (uint(i) % 64) }
func (b bitset) has(i int) bool { return b[i/64]&(1<<(uint(i)%64)) != 0 }

func (b bitset) fill() {
	for i := range b {
		b[i] = ^uint64(0)
	}
}

func (b bitset) equal(o bitset) bool {
	for i := range b {
		if b[i] != o[i] {
			return false
		}
	}
	return true
}

type variableFlow struct {
	graph      *Graph
	defs       map[NodeType]NodeDefinition
	index      map[string]int
	incoming   [][]flowEdge
	outgoing   [][]flowEdge
	reached    []bool
	provisions []provision
	nodeGen    []bitset
	edgeGen    [][]int
	exact      map[string][]int
	open       map[string][]int
	families   []int
	must       []bitset
	may        []bitset
}

func newVariableFlow(g *Graph, defs map[NodeType]NodeDefinition, sources VariableSources) *variableFlow {
	f := &variableFlow{
		graph:    g,
		defs:     defs,
		index:    make(map[string]int, len(g.Nodes)),
		incoming: make([][]flowEdge, len(g.Nodes)),
		outgoing: make([][]flowEdge, len(g.Nodes)),
		exact:    map[string][]int{},
		open:     map[string][]int{},
	}
	for i := range g.Nodes {
		if _, dup := f.index[g.Nodes[i].ID]; !dup {
			f.index[g.Nodes[i].ID] = i
		}
	}
	for _, e := range g.Edges {
		from, okFrom := f.index[e.Source]
		to, okTo := f.index[e.Target]
		if !okFrom || !okTo || g.Nodes[to].Type.IsDecoration() {
			continue
		}
		edge := flowEdge{from: from, to: to, label: e.Label}
		f.outgoing[from] = append(f.outgoing[from], edge)
		f.incoming[to] = append(f.incoming[to], edge)
	}
	f.reach()
	f.collectProvisions(sources)
	f.solve()
	return f
}

func (f *variableFlow) reach() {
	nodeMap := make(map[string]*Node, len(f.graph.Nodes))
	outgoing := make(map[string][]string, len(f.graph.Nodes))
	for i := range f.graph.Nodes {
		nodeMap[f.graph.Nodes[i].ID] = &f.graph.Nodes[i]
	}
	for _, e := range f.graph.Edges {
		outgoing[e.Source] = append(outgoing[e.Source], e.Target)
	}
	visited := reachableFromTriggers(f.graph.TriggerNodes(), nodeMap, outgoing)
	f.reached = make([]bool, len(f.graph.Nodes))
	for i := range f.graph.Nodes {
		f.reached[i] = visited[f.graph.Nodes[i].ID]
	}
}

func (f *variableFlow) add(p provision) {
	seq := len(f.provisions)
	f.provisions = append(f.provisions, p)
	switch p.match {
	case matchExact:
		f.exact[p.ref] = append(f.exact[p.ref], seq)
	case matchOpen:
		f.open[p.ref] = append(f.open[p.ref], seq)
	default:
		f.families = append(f.families, seq)
	}
}

func (f *variableFlow) reversePostorder() []int {
	visited := make([]bool, len(f.graph.Nodes))
	post := make([]int, 0, len(f.graph.Nodes))
	type frame struct {
		node int
		next int
	}
	for root := range f.graph.Nodes {
		if !f.graph.Nodes[root].Type.IsTrigger() || visited[root] {
			continue
		}
		visited[root] = true
		stack := []frame{{node: root}}
		for len(stack) > 0 {
			top := &stack[len(stack)-1]
			if top.next < len(f.outgoing[top.node]) {
				to := f.outgoing[top.node][top.next].to
				top.next++
				if !visited[to] {
					visited[to] = true
					stack = append(stack, frame{node: to})
				}
				continue
			}
			post = append(post, top.node)
			stack = stack[:len(stack)-1]
		}
	}
	for i, j := 0, len(post)-1; i < j; i, j = i+1, j-1 {
		post[i], post[j] = post[j], post[i]
	}
	return post
}

func (f *variableFlow) solve() {
	size := len(f.provisions)
	f.nodeGen = make([]bitset, len(f.graph.Nodes))
	f.edgeGen = make([][]int, len(f.graph.Nodes))
	for i := range f.graph.Nodes {
		f.nodeGen[i] = newBitset(size)
	}
	for seq, p := range f.provisions {
		if p.edge == nil {
			f.nodeGen[p.node].set(seq)
			continue
		}
		f.edgeGen[p.node] = append(f.edgeGen[p.node], seq)
	}
	order := f.reversePostorder()
	f.must = f.fixpoint(order, size, true)
	f.may = f.fixpoint(order, size, false)
}

func (f *variableFlow) edgeOut(in bitset, e flowEdge, size int) bitset {
	out := newBitset(size)
	for i := range out {
		out[i] = in[i] | f.nodeGen[e.from][i]
	}
	for _, seq := range f.edgeGen[e.from] {
		if f.provisions[seq].edge(e.label) {
			out.set(seq)
		}
	}
	return out
}

func (f *variableFlow) fixpoint(order []int, size int, must bool) []bitset {
	in := make([]bitset, len(f.graph.Nodes))
	for i := range in {
		in[i] = newBitset(size)
		if must && f.reached[i] && !f.graph.Nodes[i].Type.IsTrigger() {
			in[i].fill()
		}
	}
	for changed := true; changed; {
		changed = false
		for _, n := range order {
			if f.graph.Nodes[n].Type.IsTrigger() {
				continue
			}
			next := newBitset(size)
			if must {
				next.fill()
			}
			for _, e := range f.incoming[n] {
				if !f.reached[e.from] {
					continue
				}
				out := f.edgeOut(in[e.from], e, size)
				for i := range next {
					if must {
						next[i] &= out[i]
					} else {
						next[i] |= out[i]
					}
				}
			}
			if !next.equal(in[n]) {
				in[n] = next
				changed = true
			}
		}
	}
	return in
}

func (f *variableFlow) matching(ref string) []int {
	var seqs []int
	seqs = append(seqs, f.exact[ref]...)
	for i := len(ref); i > 0; i = strings.LastIndexByte(ref[:i], '.') {
		seqs = append(seqs, f.open[ref[:i]]...)
	}
	for _, seq := range f.families {
		p := f.provisions[seq]
		switch {
		case p.match == matchFamily && strings.HasPrefix(ref, p.ref):
			seqs = append(seqs, seq)
		case p.match == matchAnyVariable && !reservedScope(ref):
			seqs = append(seqs, seq)
		}
	}
	return seqs
}

var reservedVariableScopes = []string{"last.", "node.", "ai.", "sys.", SecretScope + "."}

func reservedScope(ref string) bool {
	for _, scope := range reservedVariableScopes {
		if strings.HasPrefix(ref, scope) {
			return true
		}
	}
	return false
}

func (f *variableFlow) availabilityAt(idx int, ref string) VariableAvailability {
	best := availableNever
	for _, seq := range f.matching(ref) {
		if f.must[idx].has(seq) {
			return AvailableAlways
		}
		if f.may[idx].has(seq) {
			best = AvailableOnSomePaths
		}
	}
	return best
}

func canonicalRef(ref string) string {
	ref = normalizeIndexing(strings.TrimSpace(ref))
	if inner, isVar := strings.CutPrefix(ref, "var."); isVar {
		return inner
	}
	return ref
}
