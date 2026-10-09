package georef

import (
	"fmt"
	"strings"

	"vozko/domain/address"
)

type UFFile struct {
	Code  string
	State string
}

func (f UFFile) FileName() string { return fmt.Sprintf("%s_%s.zip", f.Code, f.State) }

func UFFiles() []UFFile {
	states := address.IBGEStates()
	files := make([]UFFile, len(states))
	for i, s := range states {
		files[i] = UFFile{Code: s.Code, State: s.State}
	}
	return files
}

func UFFileFor(state string) (UFFile, bool) {
	state = strings.ToUpper(strings.TrimSpace(state))
	for _, f := range UFFiles() {
		if f.State == state {
			return f, true
		}
	}
	return UFFile{}, false
}
