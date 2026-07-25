package workspace_plan_usecase

import (
	"time"
)

type clockFn func() time.Time

func utcNow() time.Time {
	return time.Now().UTC()
}
