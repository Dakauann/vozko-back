package lead

import (
	"sync"
	"testing"

	gormschema "gorm.io/gorm/schema"
)

func TestASampleRowReadsEveryPointColumnTheSampleQuerySelects(t *testing.T) {
	parsed, err := gormschema.Parse(&sampleRow{}, &sync.Map{}, gormschema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	for _, column := range []string{"lat", "lng", "placement", "people", "precision_rank", "lead_ids", "value", "positions", "located"} {
		if parsed.LookUpField(column) == nil {
			t.Fatalf("column %q of the sample query is never read into the row", column)
		}
	}
}
