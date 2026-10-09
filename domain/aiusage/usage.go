package aiusage

type Tokens struct {
	Input      int64
	Output     int64
	CacheRead  int64
	CacheWrite int64
	Reasoning  int64
}

func (t Tokens) Total() int64 {
	return t.Input + t.Output
}

type Record struct {
	ReferenceID string
	WorkspaceID string
	Model       string
	Tokens      Tokens
	Billed      bool
}

func (r Record) IsModelCall() bool {
	return r.Tokens.Total() > 0
}

type Totals struct {
	Calls  int
	Billed int
	Tokens Tokens
}

func (t Totals) Covers(charges int) bool {
	return t.Billed == charges
}

type Recorder interface {
	Record(record Record) error
}

type Totaler interface {
	TotalsUnder(workspaceID, referencePrefix string) (Totals, error)
}
