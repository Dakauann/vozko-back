package balance_usecase

import (
	"context"
	"fmt"
	"time"

	"vozko/domain/balance"
	"vozko/domain/emailsending"
	"vozko/domain/shared"
	workspace_pricing "vozko/domain/workspace/workspace_pricing"
)

type EmailPrice struct {
	Service     string  `json:"service"`
	Metric      string  `json:"metric"`
	UnitMicros  int64   `json:"unitMicros"`
	UnitBRL     float64 `json:"unitBRL"`
	ThousandBRL float64 `json:"thousandBRL"`
}

type EmailReceivePrices struct {
	PerMessage EmailPrice `json:"perMessage"`
	PerChunk   EmailPrice `json:"perChunk"`
	ChunkBytes int64      `json:"chunkBytes"`
}

type EmailChunkPrices struct {
	PerChunk   EmailPrice `json:"perChunk"`
	ChunkBytes int64      `json:"chunkBytes"`
}

type EmailPriceListResult struct {
	ExchangeRate          float64            `json:"exchangeRate"`
	ExchangeRateUpdatedAt time.Time          `json:"exchangeRateUpdatedAt"`
	Send                  EmailPrice         `json:"send"`
	SendAttachments       EmailChunkPrices   `json:"sendAttachments"`
	Receive               EmailReceivePrices `json:"receive"`
}

type EmailPriceList struct {
	pricer EmailPricer
	rate   ExchangeRateReader
}

func NewEmailPriceList(pricer EmailPricer, rate ExchangeRateReader) *EmailPriceList {
	return &EmailPriceList{pricer: pricer, rate: rate}
}

func (uc *EmailPriceList) Execute(_ context.Context, workspaceID string) (EmailPriceListResult, error) {
	fx, err := ReadExchangeRate(uc.rate)
	if err != nil {
		return EmailPriceListResult{}, err
	}
	rate := fx.Rate
	out := EmailPriceListResult{
		ExchangeRate:          rate,
		ExchangeRateUpdatedAt: fx.UpdatedAt,
		SendAttachments:       EmailChunkPrices{ChunkBytes: emailsending.OutboundChunkBytes},
		Receive:               EmailReceivePrices{ChunkBytes: emailsending.InboundChunkBytes},
	}
	for line, target := range map[workspace_pricing.EmailPriceLine]*EmailPrice{
		workspace_pricing.EmailSendPerMessage:    &out.Send,
		workspace_pricing.EmailSendPerChunk:      &out.SendAttachments.PerChunk,
		workspace_pricing.EmailReceivePerMessage: &out.Receive.PerMessage,
		workspace_pricing.EmailReceivePerChunk:   &out.Receive.PerChunk,
	} {
		price, err := uc.price(workspaceID, line, rate)
		if err != nil {
			return EmailPriceListResult{}, err
		}
		*target = price
	}
	return out, nil
}

func (uc *EmailPriceList) price(workspaceID string, line workspace_pricing.EmailPriceLine, rate float64) (EmailPrice, error) {
	result, err := emailLinePrice(uc.pricer, workspaceID, line)
	if err != nil {
		return EmailPrice{}, err
	}
	return EmailPrice{
		Service:     line.Service,
		Metric:      line.Metric,
		UnitMicros:  result.PriceMicros,
		UnitBRL:     shared.EstimateBRLFromMicros(result.PriceMicros, rate),
		ThousandBRL: shared.EstimateBRLFromMicros(result.PriceMicros*1000, rate),
	}, nil
}

func emailLinePrice(pricer EmailPricer, workspaceID string, line workspace_pricing.EmailPriceLine) (workspace_pricing.PriceResult, error) {
	result, err := pricer.PriceEmail(workspaceID, line)
	if err != nil {
		return workspace_pricing.PriceResult{}, fmt.Errorf("%w: email %s/%s: %v", balance.ErrPriceUnavailable, line.Service, line.Metric, err)
	}
	if result.PriceMicros <= 0 {
		return workspace_pricing.PriceResult{}, fmt.Errorf("%w: email %s/%s", balance.ErrPriceUnavailable, line.Service, line.Metric)
	}
	return result, nil
}
