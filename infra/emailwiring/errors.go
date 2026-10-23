package emailwiring

import "errors"

var ErrIncomplete = errors.New("emailwiring: a required dependency is missing")
