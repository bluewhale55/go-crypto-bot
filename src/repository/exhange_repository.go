package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/redis/go-redis/v9"
	model "gitlab.com/open-soft/go-crypto-bot/src/model"
	"log"
	"slices"
	"strings"
	"time"
)

type SwapPairRepositoryInterface interface {
	CreateSwapPair(swapPair model.SwapPair) (*int64, error)
	UpdateSwapPair(swapPair model.SwapPair) error
	GetSwapPairs() []model.SwapPair
	GetSwapPairsByBaseAsset(baseAsset string) []model.SwapPair
	GetSwapPairsByQuoteAsset(quoteAsset string) []model.SwapPair
	GetSwapPair(symbol string) (model.SwapPair, error)
}

type ExchangeTradeInfoInterface interface {
	GetLastKLine(symbol string) *model.KLine
	GetTradeLimit(symbol string) (model.TradeLimit, error)
	GetPeriodMinPrice(symbol string, period int64) float64
	GetPredict(symbol string) (float64, error)
	GetInterpolation(kLine model.KLine) (model.Interpolation, error)
}

type ExchangeRepositoryInterface interface {
	GetSubscribedSymbols() []model.Symbol
	GetTradeLimits() []model.TradeLimit
	GetTradeLimit(symbol string) (model.TradeLimit, error)
	CreateTradeLimit(limit model.TradeLimit) (*int64, error)
	CreateSwapPair(swapPair model.SwapPair) (*int64, error)
	UpdateSwapPair(swapPair model.SwapPair) error
	GetSwapPairs() []model.SwapPair
	GetSwapPairsByBaseAsset(baseAsset string) []model.SwapPair
	GetSwapPairsByQuoteAsset(quoteAsset string) []model.SwapPair
	GetSwapPair(symbol string) (model.SwapPair, error)
	UpdateTradeLimit(limit model.TradeLimit) error
	GetLastKLine(symbol string) *model.KLine
	AddKLine(kLine model.KLine)
	KLineList(symbol string, reverse bool, size int64) []model.KLine
	GetPeriodMaxPrice(symbol string, period int64) float64
	GetPeriodMinPrice(symbol string, period int64) float64
	SetDepth(depth model.Depth)
	GetDepth(symbol string) model.Depth
	AddTrade(trade model.Trade)
	TradeList(symbol string) []model.Trade
	SetDecision(decision model.Decision, symbol string)
	GetDecision(strategy string, symbol string) *model.Decision
	GetDecisions(symbol string) []model.Decision
}

type ExchangePriceStorageInterface interface {
	GetLastKLine(symbol string) *model.KLine
	GetPeriodMinPrice(symbol string, period int64) float64
	GetDepth(symbol string) model.Depth
	SetDepth(depth model.Depth)
	GetPredict(symbol string) (float64, error)
	GetSwapPairsByBaseAsset(baseAsset string) []model.SwapPair
	GetSwapPairsByQuoteAsset(quoteAsset string) []model.SwapPair
	GetSwapPairsByAssets(quoteAsset string, baseAsset string) (model.SwapPair, error)
}

type ExchangeRepository struct {
	DB         *sql.DB
	RDB        *redis.Client
	Ctx        *context.Context
	CurrentBot *model.Bot
}

func (e *ExchangeRepository) GetSubscribedSymbols() []model.Symbol {
	symbolSlice := make([]model.Symbol, 0)
	symbolSlice = append(symbolSlice, model.Symbol{Value: "BTCUSDT"})

	return symbolSlice
}

func (e *ExchangeRepository) GetTradeLimits() []model.TradeLimit {
	res, err := e.DB.Query(`
		SELECT
		    tl.id as Id,
		    tl.symbol as Symbol,
		    tl.usdt_limit as USDTLimit,
		    tl.min_price as MinPrice,
		    tl.min_quantity as MinQuantity,
		    tl.min_notional as MinNotional,
		    tl.min_profit_percent as MinProfitPercent,
		    tl.is_enabled as IsEnabled,
		    tl.min_price_minutes_period as MinPriceMinutesPeriod,
		    tl.frame_interval as FrameInterval,
		    tl.frame_period as FramePeriod,
		    tl.buy_price_history_check_interval as BuyPriceHistoryCheckInterval,
		    tl.buy_price_history_check_period as BuyPriceHistoryCheckPeriod,
		    tl.extra_charge_options as ExtraChargeOptions
		FROM trade_limit tl WHERE tl.bot_id = ?
	`, e.CurrentBot.Id)
	defer res.Close()

	if err != nil {
		log.Fatal(err)
	}

	list := make([]model.TradeLimit, 0)

	for res.Next() {
		var tradeLimit model.TradeLimit
		err := res.Scan(
			&tradeLimit.Id,
			&tradeLimit.Symbol,
			&tradeLimit.USDTLimit,
			&tradeLimit.MinPrice,
			&tradeLimit.MinQuantity,
			&tradeLimit.MinNotional,
			&tradeLimit.MinProfitPercent,
			&tradeLimit.IsEnabled,
			&tradeLimit.MinPriceMinutesPeriod,
			&tradeLimit.FrameInterval,
			&tradeLimit.FramePeriod,
			&tradeLimit.BuyPriceHistoryCheckInterval,
			&tradeLimit.BuyPriceHistoryCheckPeriod,
			&tradeLimit.ExtraChargeOptions,
		)

		if err != nil {
			log.Fatal(err)
		}

		list = append(list, tradeLimit)
	}

	return list
}

func (e *ExchangeRepository) GetTradeLimit(symbol string) (model.TradeLimit, error) {
	var tradeLimit model.TradeLimit
	err := e.DB.QueryRow(`
		SELECT
		    tl.id as Id,
		    tl.symbol as Symbol,
		    tl.usdt_limit as USDTLimit,
		    tl.min_price as MinPrice,
		    tl.min_quantity as MinQuantity,
		    tl.min_notional as MinNotional,
		    tl.min_profit_percent as MinProfitPercent,
		    tl.is_enabled as IsEnabled,
		    tl.min_price_minutes_period as MinPriceMinutesPeriod,
		    tl.frame_interval as FrameInterval,
		    tl.frame_period as FramePeriod,
		    tl.buy_price_history_check_interval as BuyPriceHistoryCheckInterval,
		    tl.buy_price_history_check_period as BuyPriceHistoryCheckPeriod,
		    tl.extra_charge_options as ExtraChargeOptions
		FROM trade_limit tl
		WHERE tl.symbol = ? AND tl.bot_id = ?
	`,
		symbol,
		e.CurrentBot.Id,
	).Scan(
		&tradeLimit.Id,
		&tradeLimit.Symbol,
		&tradeLimit.USDTLimit,
		&tradeLimit.MinPrice,
		&tradeLimit.MinQuantity,
		&tradeLimit.MinNotional,
		&tradeLimit.MinProfitPercent,
		&tradeLimit.IsEnabled,
		&tradeLimit.MinPriceMinutesPeriod,
		&tradeLimit.FrameInterval,
		&tradeLimit.FramePeriod,
		&tradeLimit.BuyPriceHistoryCheckInterval,
		&tradeLimit.BuyPriceHistoryCheckPeriod,
		&tradeLimit.ExtraChargeOptions,
	)
	if err != nil {
		return tradeLimit, err
	}

	return tradeLimit, nil
}

func (e *ExchangeRepository) CreateTradeLimit(limit model.TradeLimit) (*int64, error) {
	res, err := e.DB.Exec(`
		INSERT INTO trade_limit SET
		    symbol = ?,
		    usdt_limit = ?,
		    min_price = ?,
		    min_quantity = ?,
		    min_notional = ?,
		    min_profit_percent = ?,
		    is_enabled = ?,
		    min_price_minutes_period = ?,
		    frame_interval = ?,
		    frame_period = ?,
		    buy_price_history_check_interval = ?,
		    buy_price_history_check_period = ?,
		    extra_charge_options = ?,
		    bot_id = ?
	`,
		limit.Symbol,
		limit.USDTLimit,
		limit.MinPrice,
		limit.MinQuantity,
		limit.MinNotional,
		limit.MinProfitPercent,
		limit.IsEnabled,
		limit.MinPriceMinutesPeriod,
		limit.FrameInterval,
		limit.FramePeriod,
		limit.BuyPriceHistoryCheckInterval,
		limit.BuyPriceHistoryCheckPeriod,
		limit.ExtraChargeOptions,
		e.CurrentBot.Id,
	)

	if err != nil {
		log.Println(err)
		return nil, err
	}

	lastId, err := res.LastInsertId()

	return &lastId, err
}

func (e *ExchangeRepository) CreateSwapPair(swapPair model.SwapPair) (*int64, error) {
	res, err := e.DB.Exec(`
		INSERT INTO swap_pair SET
		    source_symbol = ?,
		    symbol = ?,
		    base_asset = ?,
		    quote_asset = ?,
		    buy_price = ?,
		    sell_price = ?,
		    price_timestamp = ?,
		    min_notional = ?,
		    min_quantity = ?,
		    min_price = ?,
		    sell_volume = ?,
		    buy_volume = ?,
		    daily_percent = ?
	`,
		swapPair.SourceSymbol,
		swapPair.Symbol,
		swapPair.BaseAsset,
		swapPair.QuoteAsset,
		swapPair.BuyPrice,
		swapPair.SellPrice,
		swapPair.PriceTimestamp,
		swapPair.MinNotional,
		swapPair.MinQuantity,
		swapPair.MinPrice,
		swapPair.SellVolume,
		swapPair.BuyVolume,
		swapPair.DailyPercent,
	)

	if err != nil {
		log.Printf("CreateSwapPair: %s", err.Error())
		return nil, err
	}

	lastId, err := res.LastInsertId()

	return &lastId, err
}

func (e *ExchangeRepository) UpdateSwapPair(swapPair model.SwapPair) error {
	_, err := e.DB.Exec(`
		UPDATE swap_pair sp SET
		    sp.source_symbol = ?,
		    sp.symbol = ?,
		    sp.base_asset = ?,
		    sp.quote_asset = ?,
		    sp.buy_price = ?,
		    sp.sell_price = ?,
		    sp.price_timestamp = ?,
		    sp.min_notional = ?,
		    sp.min_quantity = ?,
		    sp.min_price = ?,
		    sp.sell_volume = ?,
		    sp.buy_volume = ?,
		    sp.daily_percent = ?
		WHERE sp.id = ?
	`,
		swapPair.SourceSymbol,
		swapPair.Symbol,
		swapPair.BaseAsset,
		swapPair.QuoteAsset,
		swapPair.BuyPrice,
		swapPair.SellPrice,
		swapPair.PriceTimestamp,
		swapPair.MinNotional,
		swapPair.MinQuantity,
		swapPair.MinPrice,
		swapPair.SellVolume,
		swapPair.BuyVolume,
		swapPair.DailyPercent,
		swapPair.Id,
	)

	if err != nil {
		log.Println(err)
		return err
	}

	return nil
}

func (e *ExchangeRepository) GetSwapPairs() []model.SwapPair {
	res, err := e.DB.Query(`
		SELECT
		    sp.id as Id,
		    sp.source_symbol as SourceSymbol,
		    sp.symbol as Symbol,
		    sp.base_asset as BaseAsset,
		    sp.quote_asset as QuoteAsset,
		    sp.buy_price as BuyPrice,
		    sp.sell_price as SellPrice,
		    sp.price_timestamp as PriceTimestamp,
		    sp.min_notional as MinNotional,
		    sp.min_quantity as MinQuantity,
		    sp.min_price as MinPrice,
		    sp.sell_volume as SellVolume,
		    sp.buy_volume as BuyVolume,
		    sp.daily_percent as DailyPercent
		FROM swap_pair sp
	`)
	defer res.Close()

	if err != nil {
		log.Fatalf("GetSwapPairs: %s", err.Error())
	}

	list := make([]model.SwapPair, 0)

	for res.Next() {
		var swapPair model.SwapPair
		err := res.Scan(
			&swapPair.Id,
			&swapPair.SourceSymbol,
			&swapPair.Symbol,
			&swapPair.BaseAsset,
			&swapPair.QuoteAsset,
			&swapPair.BuyPrice,
			&swapPair.SellPrice,
			&swapPair.PriceTimestamp,
			&swapPair.MinNotional,
			&swapPair.MinQuantity,
			&swapPair.MinPrice,
			&swapPair.SellVolume,
			&swapPair.BuyVolume,
			&swapPair.DailyPercent,
		)

		if err != nil {
			log.Fatalf("GetSwapPairs: %s", err.Error())
		}

		list = append(list, swapPair)
	}

	return list
}

func (e *ExchangeRepository) GetSwapPairsByBaseAsset(baseAsset string) []model.SwapPair {
	res, err := e.DB.Query(`
		SELECT
		    sp.id as Id,
		    sp.source_symbol as SourceSymbol,
		    sp.symbol as Symbol,
		    sp.base_asset as BaseAsset,
		    sp.quote_asset as QuoteAsset,
		    sp.buy_price as BuyPrice,
		    sp.sell_price as SellPrice,
		    sp.price_timestamp as PriceTimestamp,
		    sp.min_notional as MinNotional,
		    sp.min_quantity as MinQuantity,
		    sp.min_price as MinPrice,
		    sp.sell_volume as SellVolume,
		    sp.buy_volume as BuyVolume,
		    sp.daily_percent as DailyPercent
		FROM swap_pair sp 
		WHERE sp.base_asset = ? AND sp.buy_price > sp.min_price AND sp.sell_price > sp.min_price
	`, baseAsset)
	defer res.Close()

	if err != nil {
		log.Fatalf("GetSwapPairsByBaseAsset: %s", err.Error())
	}

	list := make([]model.SwapPair, 0)

	for res.Next() {
		var swapPair model.SwapPair
		err := res.Scan(
			&swapPair.Id,
			&swapPair.SourceSymbol,
			&swapPair.Symbol,
			&swapPair.BaseAsset,
			&swapPair.QuoteAsset,
			&swapPair.BuyPrice,
			&swapPair.SellPrice,
			&swapPair.PriceTimestamp,
			&swapPair.MinNotional,
			&swapPair.MinQuantity,
			&swapPair.MinPrice,
			&swapPair.SellVolume,
			&swapPair.BuyVolume,
			&swapPair.DailyPercent,
		)

		if err != nil {
			log.Fatal(err)
		}

		list = append(list, swapPair)
	}

	return list
}

func (e *ExchangeRepository) GetSwapPairsByQuoteAsset(quoteAsset string) []model.SwapPair {
	res, err := e.DB.Query(`
		SELECT
		    sp.id as Id,
		    sp.source_symbol as SourceSymbol,
		    sp.symbol as Symbol,
		    sp.base_asset as BaseAsset,
		    sp.quote_asset as QuoteAsset,
		    sp.buy_price as BuyPrice,
		    sp.sell_price as SellPrice,
		    sp.price_timestamp as PriceTimestamp,
		    sp.min_notional as MinNotional,
		    sp.min_quantity as MinQuantity,
		    sp.min_price as MinPrice,
		    sp.sell_volume as SellVolume,
		    sp.buy_volume as BuyVolume,
		    sp.daily_percent as DailyPercent
		FROM swap_pair sp 
		WHERE sp.quote_asset = ? AND sp.buy_price > sp.min_price AND sp.sell_price > sp.min_price
	`, quoteAsset)
	defer res.Close()

	if err != nil {
		log.Fatalf("GetSwapPairsByQuoteAsset: %s", err.Error())
	}

	list := make([]model.SwapPair, 0)

	for res.Next() {
		var swapPair model.SwapPair
		err := res.Scan(
			&swapPair.Id,
			&swapPair.SourceSymbol,
			&swapPair.Symbol,
			&swapPair.BaseAsset,
			&swapPair.QuoteAsset,
			&swapPair.BuyPrice,
			&swapPair.SellPrice,
			&swapPair.PriceTimestamp,
			&swapPair.MinNotional,
			&swapPair.MinQuantity,
			&swapPair.MinPrice,
			&swapPair.SellVolume,
			&swapPair.BuyVolume,
			&swapPair.DailyPercent,
		)

		if err != nil {
			log.Fatal(err)
		}

		list = append(list, swapPair)
	}

	return list
}

func (e *ExchangeRepository) GetSwapPairsByAssets(quoteAsset string, baseAsset string) (model.SwapPair, error) {
	// todo: cache...

	var swapPair model.SwapPair
	err := e.DB.QueryRow(`
		SELECT
		    sp.id as Id,
		    sp.source_symbol as SourceSymbol,
		    sp.symbol as Symbol,
		    sp.base_asset as BaseAsset,
		    sp.quote_asset as QuoteAsset,
		    sp.buy_price as BuyPrice,
		    sp.sell_price as SellPrice,
		    sp.price_timestamp as PriceTimestamp,
		    sp.min_notional as MinNotional,
		    sp.min_quantity as MinQuantity,
		    sp.min_price as MinPrice,
		    sp.sell_volume as SellVolume,
		    sp.buy_volume as BuyVolume,
		    sp.daily_percent as DailyPercent
		FROM swap_pair sp 
		WHERE sp.quote_asset = ? AND sp.base_asset = ? AND sp.buy_price > sp.min_price AND sp.sell_price > sp.min_price
	`, quoteAsset, baseAsset).Scan(
		&swapPair.Id,
		&swapPair.SourceSymbol,
		&swapPair.Symbol,
		&swapPair.BaseAsset,
		&swapPair.QuoteAsset,
		&swapPair.BuyPrice,
		&swapPair.SellPrice,
		&swapPair.PriceTimestamp,
		&swapPair.MinNotional,
		&swapPair.MinQuantity,
		&swapPair.MinPrice,
		&swapPair.SellVolume,
		&swapPair.BuyVolume,
		&swapPair.DailyPercent,
	)

	if err != nil {
		return swapPair, err
	}

	return swapPair, nil
}

func (e *ExchangeRepository) GetSwapPair(symbol string) (model.SwapPair, error) {
	var swapPair model.SwapPair
	err := e.DB.QueryRow(`
		SELECT
		    sp.id as Id,
		    sp.source_symbol as SourceSymbol,
		    sp.symbol as Symbol,
		    sp.base_asset as BaseAsset,
		    sp.quote_asset as QuoteAsset,
		    sp.buy_price as BuyPrice,
		    sp.sell_price as SellPrice,
		    sp.price_timestamp as PriceTimestamp,
		    sp.min_notional as MinNotional,
		    sp.min_quantity as MinQuantity,
		    sp.min_price as MinPrice,
		    sp.sell_volume as SellVolume,
		    sp.buy_volume as BuyVolume,
		    sp.daily_percent as DailyPercent
		FROM swap_pair sp
		WHERE sp.symbol = ?
	`,
		symbol,
	).Scan(
		&swapPair.Id,
		&swapPair.SourceSymbol,
		&swapPair.Symbol,
		&swapPair.BaseAsset,
		&swapPair.QuoteAsset,
		&swapPair.BuyPrice,
		&swapPair.SellPrice,
		&swapPair.PriceTimestamp,
		&swapPair.MinNotional,
		&swapPair.MinQuantity,
		&swapPair.MinPrice,
		&swapPair.SellVolume,
		&swapPair.BuyVolume,
		&swapPair.DailyPercent,
	)

	if err != nil {
		log.Printf("GetSwapPairsByQuoteAsset: %s", err.Error())
		return swapPair, err
	}

	return swapPair, nil
}

func (e *ExchangeRepository) UpdateTradeLimit(limit model.TradeLimit) error {
	_, err := e.DB.Exec(`
		UPDATE trade_limit tl SET
		    tl.symbol = ?,
		    tl.usdt_limit = ?,
		    tl.min_price = ?,
		    tl.min_quantity = ?,
		    tl.min_notional = ?,
		    tl.min_profit_percent = ?,
		    tl.is_enabled = ?,
		    tl.min_price_minutes_period = ?,
		    tl.frame_interval = ?,
		    tl.frame_period = ?,
		    tl.buy_price_history_check_interval = ?,
		    tl.buy_price_history_check_period = ?,
		    tl.extra_charge_options = ?
		WHERE tl.id = ?
	`,
		limit.Symbol,
		limit.USDTLimit,
		limit.MinPrice,
		limit.MinQuantity,
		limit.MinNotional,
		limit.MinProfitPercent,
		limit.IsEnabled,
		limit.MinPriceMinutesPeriod,
		limit.FrameInterval,
		limit.FramePeriod,
		limit.BuyPriceHistoryCheckInterval,
		limit.BuyPriceHistoryCheckPeriod,
		limit.ExtraChargeOptions,
		limit.Id,
	)

	if err != nil {
		log.Println(err)
		return err
	}

	return nil
}

func (e *ExchangeRepository) GetLastKLine(symbol string) *model.KLine {
	encodedLast := e.RDB.Get(*e.Ctx, fmt.Sprintf("last-kline-%s-%d", symbol, e.CurrentBot.Id)).Val()

	if len(encodedLast) > 0 {
		var dto model.KLine
		json.Unmarshal([]byte(encodedLast), &dto)

		return &dto
	}

	list := e.KLineList(symbol, false, 1)

	if len(list) > 0 {
		return &list[0]
	}

	return nil
}

func (e *ExchangeRepository) AddKLine(kLine model.KLine) {
	lastKLines := e.KLineList(kLine.Symbol, false, 200)
	encoded, _ := json.Marshal(kLine)

	for _, lastKline := range lastKLines {
		if lastKline.Timestamp == kLine.Timestamp {
			e.RDB.LPop(*e.Ctx, fmt.Sprintf("k-lines-%s-%d", kLine.Symbol, e.CurrentBot.Id)).Val()
		}
	}

	e.RDB.LPush(*e.Ctx, fmt.Sprintf("k-lines-%s-%d", kLine.Symbol, e.CurrentBot.Id), string(encoded))
	e.RDB.LTrim(*e.Ctx, fmt.Sprintf("k-lines-%s-%d", kLine.Symbol, e.CurrentBot.Id), 0, 2880)
	e.RDB.Set(*e.Ctx, fmt.Sprintf("last-kline-%s-%d", kLine.Symbol, e.CurrentBot.Id), string(encoded), time.Hour)
}

func (e *ExchangeRepository) KLineList(symbol string, reverse bool, size int64) []model.KLine {
	res := e.RDB.LRange(*e.Ctx, fmt.Sprintf("k-lines-%s-%d", symbol, e.CurrentBot.Id), 0, size).Val()
	list := make([]model.KLine, 0)

	for _, str := range res {
		var dto model.KLine
		json.Unmarshal([]byte(str), &dto)
		list = append(list, dto)
	}

	if reverse {
		slices.Reverse(list)
	}

	return list
}

func (e *ExchangeRepository) GetPeriodMaxPrice(symbol string, period int64) float64 {
	kLines := e.KLineList(symbol, true, period)
	maxPrice := 0.00
	for _, kLine := range kLines {
		if maxPrice < kLine.High {
			maxPrice = kLine.High
		}
	}

	return maxPrice
}

func (e *ExchangeRepository) GetPeriodMinPrice(symbol string, period int64) float64 {
	kLines := e.KLineList(symbol, true, period)
	minPrice := 0.00
	for _, kLine := range kLines {
		if 0.00 == minPrice || kLine.Low < minPrice {
			minPrice = kLine.Low
		}
	}

	return minPrice
}

func (e *ExchangeRepository) SetDepth(depth model.Depth) {
	encoded, _ := json.Marshal(depth)
	e.RDB.Set(*e.Ctx, fmt.Sprintf("depth-%s", depth.Symbol), string(encoded), time.Second*25)
}

func (e *ExchangeRepository) GetDepth(symbol string) model.Depth {
	res := e.RDB.Get(*e.Ctx, fmt.Sprintf("depth-%s", symbol)).Val()
	if len(res) == 0 {
		return model.Depth{
			Asks:      make([][2]model.Number, 0),
			Bids:      make([][2]model.Number, 0),
			Symbol:    symbol,
			Timestamp: time.Now().UnixMilli(),
		}
	}

	var dto model.Depth
	json.Unmarshal([]byte(res), &dto)

	return dto
}

func (e *ExchangeRepository) GetTradeVolumes(kLine model.KLine) (float64, float64) {
	buyVolume := 0.00
	sellVolume := 0.00

	for _, trade := range e.TradeList(kLine.Symbol) {
		if trade.Timestamp >= (time.Now().UnixMilli() - 60000) {
			if trade.GetOperation() == "BUY" {
				buyVolume += trade.Price * trade.Quantity
			} else {
				sellVolume += trade.Price * trade.Quantity
			}
			continue
		}

		break
	}

	return buyVolume, sellVolume
}

func (e *ExchangeRepository) AddTrade(trade model.Trade) {
	tradeCacheKey := fmt.Sprintf("trades-%s-%s", trade.Symbol, e.CurrentBot.Id)

	lastTrades := e.TradeList(trade.Symbol)
	encoded, _ := json.Marshal(trade)

	for _, lastTrade := range lastTrades {
		if lastTrade.AggregateTradeId == trade.AggregateTradeId {
			e.RDB.LPop(*e.Ctx, tradeCacheKey).Val()
		}
	}

	e.RDB.LPush(*e.Ctx, tradeCacheKey, string(encoded))
	e.RDB.LTrim(*e.Ctx, tradeCacheKey, 0, 2000)
}

func (e *ExchangeRepository) TradeList(symbol string) []model.Trade {
	tradeCacheKey := fmt.Sprintf("trades-%s-%s", symbol, e.CurrentBot.Id)
	res := e.RDB.LRange(*e.Ctx, tradeCacheKey, 0, 2000).Val()
	list := make([]model.Trade, 0)

	for _, str := range res {
		var dto model.Trade
		json.Unmarshal([]byte(str), &dto)
		list = append(list, dto)
	}

	return list
}

func (e *ExchangeRepository) SetDecision(decision model.Decision, symbol string) {
	encoded, _ := json.Marshal(decision)
	e.RDB.Set(*e.Ctx, fmt.Sprintf("decision-%s-%s-bot-%d", decision.StrategyName, symbol, e.CurrentBot.Id), string(encoded), time.Second*model.PriceValidSeconds)
}

func (e *ExchangeRepository) DeleteDecision(strategy string, symbol string) {
	e.RDB.Del(*e.Ctx, fmt.Sprintf("decision-%s-%s-bot-%d", strategy, symbol, e.CurrentBot.Id))
}

func (e *ExchangeRepository) GetDecision(strategy string, symbol string) *model.Decision {
	res := e.RDB.Get(*e.Ctx, fmt.Sprintf("decision-%s-%s-bot-%d", strategy, symbol, e.CurrentBot.Id)).Val()
	if len(res) == 0 {
		return nil
	}

	var dto model.Decision
	json.Unmarshal([]byte(res), &dto)

	return &dto
}

func (e *ExchangeRepository) getPredictedCacheKey(symbol string) string {
	return fmt.Sprintf("predicted-price-%s-%d", symbol, e.CurrentBot.Id)
}

func (e *ExchangeRepository) GetPredict(symbol string) (float64, error) {
	var predictedPrice float64

	predictedPriceCacheKey := e.getPredictedCacheKey(symbol)
	predictedPriceCached := e.RDB.Get(*e.Ctx, predictedPriceCacheKey).Val()

	if len(predictedPriceCached) > 0 {
		_ = json.Unmarshal([]byte(predictedPriceCached), &predictedPrice)
		return predictedPrice, nil
	}

	return 0.00, errors.New("predict is not found")
}

func (e *ExchangeRepository) SavePredict(predicted float64, symbol string) {
	predictedPriceCacheKey := e.getPredictedCacheKey(symbol)

	encoded, _ := json.Marshal(predicted)
	e.RDB.Set(*e.Ctx, predictedPriceCacheKey, string(encoded), time.Minute)
}

func (e *ExchangeRepository) GetKLinePredict(kLine model.KLine) (float64, error) {
	var predictedPrice float64

	predictedPriceCacheKey := fmt.Sprintf("%s-%d", e.getPredictedCacheKey(kLine.Symbol), kLine.Timestamp)
	predictedPriceCached := e.RDB.Get(*e.Ctx, predictedPriceCacheKey).Val()

	if len(predictedPriceCached) > 0 {
		_ = json.Unmarshal([]byte(predictedPriceCached), &predictedPrice)
		return predictedPrice, nil
	}

	return 0.00, errors.New("predict is not found")
}

func (e *ExchangeRepository) SaveKLinePredict(predicted float64, kLine model.KLine) {
	predictedPriceCacheKey := fmt.Sprintf("%s-%d", e.getPredictedCacheKey(kLine.Symbol), kLine.Timestamp)

	encoded, _ := json.Marshal(predicted)
	e.RDB.Set(*e.Ctx, predictedPriceCacheKey, string(encoded), time.Minute*600)
}

func (e *ExchangeRepository) getInterpolationCacheKey(symbol string) string {
	return fmt.Sprintf("interpolation-price-%s-%d", symbol, e.CurrentBot.Id)
}
func (e *ExchangeRepository) GetInterpolation(kLine model.KLine) (model.Interpolation, error) {
	var interpolation model.Interpolation

	cacheKey := fmt.Sprintf("%s-%d", e.getInterpolationCacheKey(kLine.Symbol), kLine.Timestamp)
	interpolationCached := e.RDB.Get(*e.Ctx, cacheKey).Val()

	if len(interpolationCached) > 0 {
		_ = json.Unmarshal([]byte(interpolationCached), &interpolation)
		return interpolation, nil
	}

	return model.Interpolation{
		Asset:                strings.ReplaceAll(kLine.Symbol, "USDT", ""),
		EthInterpolationUsdt: 0.00,
		BtcInterpolationUsdt: 0.00,
	}, errors.New("interpolation is not found")
}

func (e *ExchangeRepository) SaveInterpolation(interpolation model.Interpolation, kLine model.KLine) {
	cacheKey := fmt.Sprintf("%s-%d", e.getInterpolationCacheKey(kLine.Symbol), kLine.Timestamp)

	encoded, _ := json.Marshal(interpolation)
	e.RDB.Set(*e.Ctx, cacheKey, string(encoded), time.Minute*600)
}

func (e *ExchangeRepository) GetDecisions(symbol string) []model.Decision {
	currentDecisions := make([]model.Decision, 0)
	smaDecision := e.GetDecision(model.SmaTradeStrategyName, symbol)
	kLineDecision := e.GetDecision(model.BaseKlineStrategyName, symbol)
	marketDepthDecision := e.GetDecision(model.MarketDepthStrategyName, symbol)
	orderBasedDecision := e.GetDecision(model.OrderBasedStrategyName, symbol)

	if smaDecision != nil {
		currentDecisions = append(currentDecisions, *smaDecision)
	}
	if kLineDecision != nil {
		currentDecisions = append(currentDecisions, *kLineDecision)
	}
	if marketDepthDecision != nil {
		currentDecisions = append(currentDecisions, *marketDepthDecision)
	}
	if orderBasedDecision != nil {
		currentDecisions = append(currentDecisions, *orderBasedDecision)
	}

	return currentDecisions
}
