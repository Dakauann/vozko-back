package mercadolivre

import "time"

func SetPurgeClock(job *PurgeJob, now func() time.Time) { job.now = now }
