package shared

import "errors"

var (
	ErrVersionRequired = errors.New("the version that was read is required")
	ErrVersionConflict = errors.New("the record was changed after it was read")
)

func RequireVersion(expected int64) error {
	if expected < 1 {
		return ErrVersionRequired
	}
	return nil
}

func ExpectVersion(current int64, expected *int64) error {
	if expected == nil {
		return ErrVersionRequired
	}
	if err := RequireVersion(*expected); err != nil {
		return err
	}
	if *expected != current {
		return ErrVersionConflict
	}
	return nil
}
