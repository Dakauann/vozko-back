package customer

type DocumentValidator interface {
	ValidateCPFOrCNPJ(document string) bool
	Normalize(document string) string
}
