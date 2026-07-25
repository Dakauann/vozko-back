package workspace_addon_usecase

import "time"

type clockFn func() time.Time

func utcNow() time.Time { return time.Now().UTC() }

const balanceCurrencyUSD = "USD"
