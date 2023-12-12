package model

import (
	"math"
	"strings"
	"time"
)

type Percent float64

func (p Percent) IsPositive() bool {
	return float64(p) > 0
}

func (p Percent) Value() float64 {
	return float64(p)
}

func (p Percent) Half() Percent {
	return Percent(float64(p) / 2)
}

func (p Percent) Gt(percent Percent) bool {
	return p.Value() > percent.Value()
}

func (p Percent) Gte(percent Percent) bool {
	return p.Value() >= percent.Value()
}

func (p Percent) Lte(percent Percent) bool {
	return p.Value() <= percent.Value()
}

type Order struct {
	Id               int64    `json:"id"`
	Symbol           string   `json:"symbol"`
	Price            float64  `json:"price"`
	Quantity         float64  `json:"quantity"`
	ExecutedQuantity float64  `json:"executedQuantity"`
	CreatedAt        string   `json:"createdAt"`
	SellVolume       float64  `json:"sellVolume"`
	BuyVolume        float64  `json:"buyVolume"`
	SmaValue         float64  `json:"smaValue"`
	Operation        string   `json:"operation"`
	Status           string   `json:"status"`
	ExternalId       *int64   `json:"externalId"`
	ClosesOrder      *int64   `json:"closesOrder"` // sell order here
	UsedExtraBudget  float64  `json:"usedExtraBudget"`
	Commission       *float64 `json:"commission"`
	CommissionAsset  *string  `json:"commissionAsset"`
	SoldQuantity     *float64 `json:"soldQuantity"`
}

func (o *Order) GetBaseAsset() string {
	return strings.ReplaceAll(o.Symbol, "USDT", "")
}

func (o *Order) GetHoursOpened() int64 {
	date, _ := time.Parse("2006-01-02 15:04:05", o.CreatedAt)

	return (time.Now().Unix() - date.Unix()) / 3600
}

func (o *Order) GetProfitPercent(currentPrice float64) Percent {
	return Percent(math.Round((currentPrice-o.Price)*100/o.Price*100) / 100)
}

func (o *Order) GetMinClosePrice(limit TradeLimit) float64 {
	return o.Price * (100 + limit.GetMinProfitPercent().Value()) / 100
}

func (o *Order) IsSell() bool {
	return o.Operation == "SELL"
}

func (o *Order) IsBuy() bool {
	return o.Operation == "BUY"
}

func (o *Order) IsClosed() bool {
	return o.Status == "closed"
}

func (o *Order) GetRemainingToSellQuantity() float64 {
	if o.SoldQuantity != nil {
		return o.ExecutedQuantity - *o.SoldQuantity
	}

	return o.ExecutedQuantity
}
