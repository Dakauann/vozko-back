package cache

type HashFieldAcknowledger interface {
	HDelIfValue(key, field, value string) error
}
