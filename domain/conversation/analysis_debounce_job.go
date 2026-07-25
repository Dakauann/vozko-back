package conversation

type AnalysisDebounceJob interface {
	ProcessPendingAnalyses() error
}
