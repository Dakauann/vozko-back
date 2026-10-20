package mercadolivre

import (
	"context"
	"testing"
)

func TestGetPack(t *testing.T) {
	fs := newFakeServer(t, jsonReply(200, fixture(t, "pack_doc_example.json")))
	limiters := newFakeLimiters()
	s, err := NewSalesService(newTestClient(t, fs, limiters))
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.GetPack(context.Background(), sellerCreds, "2000006181551917")
	if err != nil {
		t.Fatal(err)
	}
	if fs.last().Path != "/packs/2000006181551917" {
		t.Fatalf("path = %s", fs.last().Path)
	}
	assertBearer(t, fs.last())
	if p.ID != "2000006181551917" || p.Status != "released" || p.BuyerID != "1944693439" || p.ShipmentID != "43729529445" ||
		len(p.OrderIDs) != 2 || p.OrderIDs[0] != "2000009047722568" || p.OrderIDs[1] != "2000009047707726" {
		t.Fatalf("pack = %+v", p)
	}
	if keys := limiters.keys(); keys[1] != "ml:app:other" {
		t.Fatalf("limiter keys = %v", keys)
	}
	if _, err := s.GetPack(context.Background(), sellerCreds, "x"); err == nil {
		t.Fatal("malformed pack accepted")
	}
}

func TestGetOrder(t *testing.T) {
	fs := newFakeServer(t, jsonReply(200, fixture(t, "order_doc_example_cleaned.json")))
	s, _ := NewSalesService(newTestClient(t, fs, newFakeLimiters()))
	o, err := s.GetOrder(context.Background(), sellerCreds, "2000009713473608")
	if err != nil {
		t.Fatal(err)
	}
	if fs.last().Path != "/orders/2000009713473608" {
		t.Fatalf("path = %s", fs.last().Path)
	}
	if o.ID != "2000009713473608" || o.PackID != "2000006556183755" || o.Status != "paid" || o.BuyerID != "6427433" ||
		o.SellerID != "779432305" || o.ShipmentID != "44028792542" || o.CurrencyID != "BRL" || o.TotalAmount != 125.92 ||
		len(o.Items) != 1 || o.Items[0].ItemID != "MLB3737188005" || o.Items[0].Quantity != 2 || o.Items[0].UnitPrice != 62.96 ||
		o.Items[0].Title == "" || len(o.Tags) != 5 {
		t.Fatalf("order = %+v", o)
	}
}

func TestGetOrderWithoutAPack(t *testing.T) {
	fs := newFakeServer(t, jsonReply(200, `{"id":2000009713473608,"pack_id":null,"status":"paid","buyer":{"id":1},"seller":{"id":2},"shipping":{"id":null},"order_items":[]}`))
	s, _ := NewSalesService(newTestClient(t, fs, newFakeLimiters()))
	o, err := s.GetOrder(context.Background(), sellerCreds, "2000009713473608")
	if err != nil {
		t.Fatal(err)
	}
	if o.PackID != "" || o.ShipmentID != "" {
		t.Fatalf("order = %+v", o)
	}
}

func TestGetShipmentSendsTheNewFormatHeader(t *testing.T) {
	fs := newFakeServer(t, jsonReply(200, fixture(t, "shipment_from_doc_schema.json")))
	s, _ := NewSalesService(newTestClient(t, fs, newFakeLimiters()))
	sh, err := s.GetShipment(context.Background(), sellerCreds, "43729529445")
	if err != nil {
		t.Fatal(err)
	}
	r := fs.last()
	if r.Path != "/shipments/43729529445" || r.Header.Get("X-Format-New") != "true" {
		t.Fatalf("%s %v", r.Path, r.Header)
	}
	if sh.ID != "43729529445" || sh.Status != "shipped" || sh.Substatus != "out_for_delivery" ||
		sh.LogisticMode != "me2" || sh.LogisticType != "drop_off" || sh.TrackingNumber != "NL123456789BR" {
		t.Fatalf("shipment = %+v", sh)
	}
}
