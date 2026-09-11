package cache

// HashFieldAcknowledger removes only the version a worker finished processing.
// A concurrent producer's replacement must remain queued.
type HashFieldAcknowledger interface {
	HDelIfValue(key, field, value string) error
}
