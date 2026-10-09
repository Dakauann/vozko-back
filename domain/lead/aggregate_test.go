package lead

import "testing"

func TestIsAggregate(t *testing.T) {
	record := &Lead{ID: "l-1"}
	if record.IsAggregate() {
		t.Fatal("a lead read without its lists is only a record")
	}
	record.EnsureCollections()
	if !record.IsAggregate() || len(record.Phones) != 0 || len(record.Addresses) != 0 || len(record.Relations) != 0 {
		t.Fatalf("EnsureCollections = %+v", record)
	}
	contactDetails := &Lead{Phones: []ContactPhone{}, Addresses: []Address{}}
	if !contactDetails.IsAggregate() {
		t.Fatal("phones and addresses make the aggregate; relations are written through the relation store")
	}
	phonesOnly := &Lead{Phones: []ContactPhone{}}
	if phonesOnly.IsAggregate() {
		t.Fatal("a lead without its addresses is not a whole aggregate")
	}
	kept := &Lead{Phones: []ContactPhone{{Number: "551133334444"}}}
	kept.EnsureCollections()
	if len(kept.Phones) != 1 {
		t.Fatal("EnsureCollections keeps what is there")
	}
}
