package emailpricing

import (
	"time"

	balance_usecase "vozko/usecases/balance"
)

type EmailPriceResponse struct {
	Service     string  `json:"service" example:"send"`
	Metric      string  `json:"metric" example:"per_message"`
	UnitMicros  int64   `json:"unitMicros" example:"583"`
	UnitBRL     float64 `json:"unitBRL" example:"0.003498"`
	ThousandBRL float64 `json:"thousandBRL" example:"3.5"`
}

type EmailReceivePricesResponse struct {
	PerMessage EmailPriceResponse `json:"perMessage"`
	PerChunk   EmailPriceResponse `json:"perChunk"`
	ChunkBytes int64              `json:"chunkBytes" example:"262144"`
}

type EmailChunkPricesResponse struct {
	PerChunk   EmailPriceResponse `json:"perChunk"`
	ChunkBytes int64              `json:"chunkBytes" example:"262144"`
}

type EmailPricingResponse struct {
	ExchangeRate          float64                    `json:"exchangeRate" example:"6"`
	ExchangeRateUpdatedAt time.Time                  `json:"exchangeRateUpdatedAt" example:"2026-10-20T09:30:00Z"`
	Send                  EmailPriceResponse         `json:"send"`
	SendAttachments       EmailChunkPricesResponse   `json:"sendAttachments"`
	Receive               EmailReceivePricesResponse `json:"receive"`
}

func toPriceResponse(p balance_usecase.EmailPrice) EmailPriceResponse {
	return EmailPriceResponse{Service: p.Service, Metric: p.Metric, UnitMicros: p.UnitMicros, UnitBRL: p.UnitBRL, ThousandBRL: p.ThousandBRL}
}

func toResponse(r balance_usecase.EmailPriceListResult) EmailPricingResponse {
	return EmailPricingResponse{
		ExchangeRate:          r.ExchangeRate,
		ExchangeRateUpdatedAt: r.ExchangeRateUpdatedAt,
		Send:                  toPriceResponse(r.Send),
		SendAttachments: EmailChunkPricesResponse{
			PerChunk:   toPriceResponse(r.SendAttachments.PerChunk),
			ChunkBytes: r.SendAttachments.ChunkBytes,
		},
		Receive: EmailReceivePricesResponse{
			PerMessage: toPriceResponse(r.Receive.PerMessage),
			PerChunk:   toPriceResponse(r.Receive.PerChunk),
			ChunkBytes: r.Receive.ChunkBytes,
		},
	}
}
