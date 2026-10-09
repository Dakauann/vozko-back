package crmfilter

type Sort struct {
	Field Field  `json:"field"`
	Key   string `json:"key,omitempty"`
	Desc  bool   `json:"desc,omitempty"`
}
