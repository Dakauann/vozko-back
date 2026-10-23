package shared

import "errors"

var ErrYesNoInvalid = errors.New("value is not a yes or no answer")

func ParseYesNo(raw string) (bool, error) {
	switch FoldForMatch(raw) {
	case "sim", "s", "yes", "y", "1", "true", "verdadeiro", "x":
		return true, nil
	case "nao", "n", "no", "0", "false", "falso":
		return false, nil
	}
	return false, ErrYesNoInvalid
}
