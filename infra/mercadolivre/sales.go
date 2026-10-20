package mercadolivre

import (
	"context"
	"errors"
	"fmt"

	mldomain "vozko/domain/mercadolivre"
)

type SalesService struct {
	c *Client
}

var (
	_ mldomain.PackService     = (*SalesService)(nil)
	_ mldomain.OrderService    = (*SalesService)(nil)
	_ mldomain.ShipmentService = (*SalesService)(nil)
)

func NewSalesService(c *Client) (*SalesService, error) {
	if c == nil {
		return nil, errors.New("mercadolivre: sales service requires a client")
	}
	return &SalesService{c: c}, nil
}

func numericResource(kind, id string) error {
	if !mldomain.ValidNumericID(id) {
		return fmt.Errorf("%w: %s %q", mldomain.ErrInvalidResource, kind, id)
	}
	return nil
}

type packWire struct {
	ID       mldomain.FlexibleID `json:"id"`
	Status   string              `json:"status"`
	Buyer    *idRef              `json:"buyer"`
	Shipment *idRef              `json:"shipment"`
	Orders   []idRef             `json:"orders"`
}

func (s *SalesService) GetPack(ctx context.Context, creds mldomain.Credentials, packID string) (*mldomain.RemotePack, error) {
	if err := numericResource("pack", packID); err != nil {
		return nil, err
	}
	var w packWire
	if err := s.c.getJSON(ctx, request{path: "/packs/" + pathEscape(packID), family: FamilyOther, creds: creds}, &w); err != nil {
		return nil, err
	}
	p := &mldomain.RemotePack{ID: w.ID.String(), Status: w.Status}
	if w.Buyer != nil {
		p.BuyerID = w.Buyer.ID.String()
	}
	if w.Shipment != nil {
		p.ShipmentID = w.Shipment.ID.String()
	}
	for _, o := range w.Orders {
		if o.ID != "" {
			p.OrderIDs = append(p.OrderIDs, o.ID.String())
		}
	}
	if !mldomain.ValidNumericID(p.ID) {
		return nil, &Error{Code: CodeDecode, Message: "pack answer carries no id"}
	}
	return p, nil
}

type orderItemWire struct {
	Item struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	} `json:"item"`
	Quantity   int     `json:"quantity"`
	UnitPrice  float64 `json:"unit_price"`
	CurrencyID string  `json:"currency_id"`
}

type orderWire struct {
	ID          mldomain.FlexibleID `json:"id"`
	PackID      mldomain.FlexibleID `json:"pack_id"`
	Status      string              `json:"status"`
	Buyer       *idRef              `json:"buyer"`
	Seller      *idRef              `json:"seller"`
	Shipping    *idRef              `json:"shipping"`
	CurrencyID  string              `json:"currency_id"`
	TotalAmount float64             `json:"total_amount"`
	OrderItems  []orderItemWire     `json:"order_items"`
	Tags        []string            `json:"tags"`
}

func (s *SalesService) GetOrder(ctx context.Context, creds mldomain.Credentials, orderID string) (*mldomain.RemoteOrder, error) {
	if err := numericResource("order", orderID); err != nil {
		return nil, err
	}
	var w orderWire
	if err := s.c.getJSON(ctx, request{path: "/orders/" + pathEscape(orderID), family: FamilyOther, creds: creds}, &w); err != nil {
		return nil, err
	}
	o := &mldomain.RemoteOrder{
		ID:          w.ID.String(),
		PackID:      w.PackID.String(),
		Status:      w.Status,
		CurrencyID:  w.CurrencyID,
		TotalAmount: w.TotalAmount,
		Tags:        w.Tags,
	}
	if w.Buyer != nil {
		o.BuyerID = w.Buyer.ID.String()
	}
	if w.Seller != nil {
		o.SellerID = w.Seller.ID.String()
	}
	if w.Shipping != nil {
		o.ShipmentID = w.Shipping.ID.String()
	}
	for _, it := range w.OrderItems {
		o.Items = append(o.Items, mldomain.RemoteOrderItem{
			ItemID:     it.Item.ID,
			Title:      it.Item.Title,
			Quantity:   it.Quantity,
			UnitPrice:  it.UnitPrice,
			CurrencyID: it.CurrencyID,
		})
	}
	if !mldomain.ValidNumericID(o.ID) {
		return nil, &Error{Code: CodeDecode, Message: "order answer carries no id"}
	}
	return o, nil
}

type shipmentWire struct {
	ID             mldomain.FlexibleID `json:"id"`
	Status         string              `json:"status"`
	Substatus      string              `json:"substatus"`
	TrackingNumber string              `json:"tracking_number"`
	Logistic       *struct {
		Mode string `json:"mode"`
		Type string `json:"type"`
	} `json:"logistic"`
}

func (s *SalesService) GetShipment(ctx context.Context, creds mldomain.Credentials, shipmentID string) (*mldomain.RemoteShipment, error) {
	if err := numericResource("shipment", shipmentID); err != nil {
		return nil, err
	}
	var w shipmentWire
	if err := s.c.getJSON(ctx, request{
		path:    "/shipments/" + pathEscape(shipmentID),
		family:  FamilyOther,
		creds:   creds,
		headers: map[string]string{"X-Format-New": "true"},
	}, &w); err != nil {
		return nil, err
	}
	sh := &mldomain.RemoteShipment{
		ID:             w.ID.String(),
		Status:         w.Status,
		Substatus:      w.Substatus,
		TrackingNumber: w.TrackingNumber,
	}
	if w.Logistic != nil {
		sh.LogisticMode, sh.LogisticType = w.Logistic.Mode, w.Logistic.Type
	}
	if !mldomain.ValidNumericID(sh.ID) {
		return nil, &Error{Code: CodeDecode, Message: "shipment answer carries no id"}
	}
	return sh, nil
}
