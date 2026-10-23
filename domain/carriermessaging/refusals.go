package carriermessaging

import (
	"errors"
	"strconv"
	"time"
)

const (
	DailyFreeTestSends = 20
)

var (
	ErrPhoneInvalid        = errors.New("rcs: the phone number is not valid")
	ErrChannelNotLaunched  = errors.New("carrier messaging: this channel is coming soon")
	ErrSuppressed          = errors.New("rcs: the contact opted out of RCS marketing")
	ErrRecipientRefused    = errors.New("rcs: the contact is blocked or opted out")
	ErrQuietHours          = errors.New("rcs: marketing messages wait for the send hours")
	ErrFrequencyCap        = errors.New("rcs: the contact already got the most marketing messages allowed")
	ErrInsufficientBalance = errors.New("rcs: the balance does not cover this send and the reply reserve")
	ErrTestLimit           = errors.New("rcs: the daily free test sends are used up")
)

type QuietHoursError struct {
	NextOpen time.Time
}

func (e *QuietHoursError) Error() string {
	return ErrQuietHours.Error() + ": next opening " + e.NextOpen.Format(time.RFC3339)
}

func (e *QuietHoursError) Is(target error) bool { return target == ErrQuietHours }

type ShortfallError struct {
	MissingMicros int64
}

func (e *ShortfallError) Error() string {
	return ErrInsufficientBalance.Error() + ": missing " + strconv.FormatInt(e.MissingMicros, 10) + " micros"
}

func (e *ShortfallError) Is(target error) bool { return target == ErrInsufficientBalance }
