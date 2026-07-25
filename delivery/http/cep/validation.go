package cep

import "strings"

func (r *CEPSearchRequest) Validate() map[string]string {
	r.CEP = strings.TrimSpace(r.CEP)
	if r.CEP == "" {
		return map[string]string{"cep": "required"}
	}
	return nil
}
