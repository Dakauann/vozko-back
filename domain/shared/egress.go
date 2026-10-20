package shared

import "errors"

var ErrEgressBlocked = errors.New("destination is a private, loopback or reserved address")
