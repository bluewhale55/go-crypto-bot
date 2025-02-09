package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/redis/go-redis/v9"
	ExchangeModel "gitlab.com/open-soft/go-crypto-bot/src/model"
	"log"
	"strings"
	"time"
)

type OrderUpdaterInterface interface {
	Update(order ExchangeModel.Order) error
}

type OrderCachedReaderInterface interface {
	GetOpenedOrderCached(symbol string, operation string) (ExchangeModel.Order, error)
}

type OrderStorageInterface interface {
	Create(order ExchangeModel.Order) (*int64, error)
	Update(order ExchangeModel.Order) error
	DeleteManualOrder(symbol string)
	Find(id int64) (ExchangeModel.Order, error)
	GetClosesOrderList(buyOrder ExchangeModel.Order) []ExchangeModel.Order
	DeleteBinanceOrder(order ExchangeModel.BinanceOrder)
	GetOpenedOrderCached(symbol string, operation string) (ExchangeModel.Order, error)
	GetManualOrder(symbol string) *ExchangeModel.ManualOrder
	SetBinanceOrder(order ExchangeModel.BinanceOrder)
	GetBinanceOrder(symbol string, operation string) *ExchangeModel.BinanceOrder
	LockBuy(symbol string, seconds int64)
	HasBuyLock(symbol string) bool
}

type OrderRepository struct {
	DB         *sql.DB
	RDB        *redis.Client
	Ctx        *context.Context
	CurrentBot *ExchangeModel.Bot
}

func (o *OrderRepository) getOpenedOrderCacheKey(symbol string, operation string) string {
	return fmt.Sprintf(
		"opened-order-%s-%s-bot-%d",
		symbol,
		strings.ToLower(operation),
		o.CurrentBot.Id,
	)
}

func (repo *OrderRepository) GetOpenedOrderCached(symbol string, operation string) (ExchangeModel.Order, error) {
	res := repo.RDB.Get(*repo.Ctx, repo.getOpenedOrderCacheKey(symbol, operation)).Val()
	if len(res) > 0 {
		var dto ExchangeModel.Order
		json.Unmarshal([]byte(res), &dto)

		if dto.ExecutedQuantity > 0 {
			return dto, nil
		}
	}

	order, err := repo.GetOpenedOrder(symbol, operation)

	if err != nil {
		return order, err
	}

	encoded, _ := json.Marshal(order)
	repo.RDB.Set(*repo.Ctx, repo.getOpenedOrderCacheKey(symbol, operation), string(encoded), time.Minute*60)

	return order, nil
}

func (repo *OrderRepository) DeleteOpenedOrderCache(order ExchangeModel.Order) {
	repo.RDB.Del(*repo.Ctx, repo.getOpenedOrderCacheKey(order.Symbol, order.Operation)).Val()
}

func (repo *OrderRepository) GetOpenedOrder(symbol string, operation string) (ExchangeModel.Order, error) {
	var order ExchangeModel.Order

	err := repo.DB.QueryRow(`
		SELECT 
			o.id as Id, 
			o.symbol as Symbol, 
			o.quantity as Quantity,
			o.executed_quantity as ExecutedQuantity,
			o.price as Price,
			o.created_at as CreatedAt,
			o.operation as Operation,
			o.status as Status,
			o.sell_volume as SellVolume,
			o.buy_volume as BuyVolume,
			o.sma_value as SmaValue,
			o.external_id as ExternalId,
			o.closes_order as ClosesOrder,
			o.used_extra_budget as UsedExtraBudget,
			o.commission as Commission,
			o.commission_asset as CommissionAsset,
			SUM(IFNULL(sell.executed_quantity, 0)) as SoldQuantity,
			o.swap as Swap,
			o.extra_charge_options as ExtraChargeOptions
		FROM orders o
		LEFT JOIN orders sell ON o.id = sell.closes_order AND sell.operation = 'SELL'
		WHERE o.status = ? AND o.symbol = ? AND o.operation = ? AND o.bot_id = ?
		GROUP BY o.id`,
		"opened",
		symbol,
		operation,
		repo.CurrentBot.Id,
	).Scan(
		&order.Id,
		&order.Symbol,
		&order.Quantity,
		&order.ExecutedQuantity,
		&order.Price,
		&order.CreatedAt,
		&order.Operation,
		&order.Status,
		&order.SellVolume,
		&order.BuyVolume,
		&order.SmaValue,
		&order.ExternalId,
		&order.ClosesOrder,
		&order.UsedExtraBudget,
		&order.Commission,
		&order.CommissionAsset,
		&order.SoldQuantity,
		&order.Swap,
		&order.ExtraChargeOptions,
	)

	if err != nil {
		return order, err
	}

	return order, nil
}

func (repo *OrderRepository) Create(order ExchangeModel.Order) (*int64, error) {
	res, err := repo.DB.Exec(`
		INSERT INTO orders SET
	  		symbol = ?,
		    quantity = ?,
		    executed_quantity = ?,
	        price = ?,
		    created_at = ?,
		    sell_volume = ?,
	        buy_volume = ?,
		    sma_value = ?,
		    operation = ?,
		    status = ?,
		    external_id = ?,
		    closes_order = ?,
			used_extra_budget = ?,
			commission = ?,
			commission_asset = ?,
			extra_charge_options = ?,
			bot_id = ?
	`,
		order.Symbol,
		order.Quantity,
		order.ExecutedQuantity,
		order.Price,
		order.CreatedAt,
		order.SellVolume,
		order.BuyVolume,
		order.SmaValue,
		order.Operation,
		order.Status,
		order.ExternalId,
		order.ClosesOrder,
		order.UsedExtraBudget,
		order.Commission,
		order.CommissionAsset,
		order.ExtraChargeOptions,
		repo.CurrentBot.Id,
	)

	if err != nil {
		log.Println(err)

		return nil, err
	}

	lastId, err := res.LastInsertId()

	return &lastId, err
}

func (repo *OrderRepository) Update(order ExchangeModel.Order) error {
	repo.DeleteOpenedOrderCache(order)
	_, err := repo.DB.Exec(`
		UPDATE orders o SET
	  		o.symbol = ?,
		    o.quantity = ?,
		    o.executed_quantity = ?,
	        o.price = ?,
		    o.created_at = ?,
		    o.sell_volume = ?,
	        o.buy_volume = ?,
		    o.sma_value = ?,
		    o.operation = ?,
		    o.status = ?,
		    o.external_id = ?,
			o.closes_order = ?,
			o.used_extra_budget = ?,
			o.commission = ?,
			o.commission_asset = ?,
			o.swap = ?,
			o.extra_charge_options = ?
		WHERE o.id = ? AND o.bot_id = ?
	`,
		order.Symbol,
		order.Quantity,
		order.ExecutedQuantity,
		order.Price,
		order.CreatedAt,
		order.SellVolume,
		order.BuyVolume,
		order.SmaValue,
		order.Operation,
		order.Status,
		order.ExternalId,
		order.ClosesOrder,
		order.UsedExtraBudget,
		order.Commission,
		order.CommissionAsset,
		order.Swap,
		order.ExtraChargeOptions,
		order.Id,
		repo.CurrentBot.Id,
	)

	if err != nil {
		log.Println(err)
		return err
	}

	return nil
}

func (repo *OrderRepository) Find(id int64) (ExchangeModel.Order, error) {
	var order ExchangeModel.Order

	err := repo.DB.QueryRow(`
		SELECT 
			o.id as Id, 
			o.symbol as Symbol, 
			o.quantity as Quantity,
			o.executed_quantity as ExecutedQuantity,
			o.price as Price,
			o.created_at as CreatedAt,
			o.operation as Operation,
			o.status as Status,
			o.sell_volume as SellVolume,
			o.buy_volume as BuyVolume,
			o.sma_value as SmaValue,
			o.external_id as ExternalId,
			o.closes_order as ClosesOrder,
			o.used_extra_budget as UsedExtraBudget,
			o.commission as Commission,
			o.commission_asset as CommissionAsset,
			SUM(IFNULL(sell.executed_quantity, 0)) as SoldQuantity,
			o.swap as Swap,
			o.extra_charge_options as ExtraChargeOptions
		FROM orders o
		LEFT JOIN orders sell ON o.id = sell.closes_order AND sell.operation = 'SELL'
		WHERE o.id = ? AND o.bot_id = ?
		GROUP BY o.id`, id, repo.CurrentBot.Id,
	).Scan(
		&order.Id,
		&order.Symbol,
		&order.Quantity,
		&order.ExecutedQuantity,
		&order.Price,
		&order.CreatedAt,
		&order.Operation,
		&order.Status,
		&order.SellVolume,
		&order.BuyVolume,
		&order.SmaValue,
		&order.ExternalId,
		&order.ClosesOrder,
		&order.UsedExtraBudget,
		&order.Commission,
		&order.CommissionAsset,
		&order.SoldQuantity,
		&order.Swap,
		&order.ExtraChargeOptions,
	)

	if err != nil {
		return order, err
	}

	return order, nil
}

func (repo *OrderRepository) GetTrades() []ExchangeModel.OrderTrade {
	res, err := repo.DB.Query(`
		SELECT
			trade.id as OrderId,
			initial.created_at as Open,
			trade.created_at as Close,
			initial.price as Buy,
			trade.price as Sell,
			trade.executed_quantity as BuyQuantity,
			trade.executed_quantity as SellQuantity,
			(trade.price * trade.executed_quantity) - (initial.price * trade.executed_quantity) as Profit,
			trade.symbol as Symbol,
			TIMESTAMPDIFF(HOUR, initial.created_at, trade.created_at) as HoursOpened,
			(initial.price * initial.executed_quantity) as Budget,
			((trade.price * trade.executed_quantity) - (initial.price * trade.executed_quantity)) * 100 / (initial.price * trade.quantity) as Percent
		FROM orders trade
		INNER JOIN orders initial ON initial.id = trade.closes_order AND initial.operation = 'buy' AND initial.bot_id = ?
		WHERE trade.operation = 'sell' and trade.status = 'closed' AND trade.bot_id = ?
		ORDER BY Close DESC
	`, repo.CurrentBot.Id, repo.CurrentBot.Id)
	defer res.Close()

	if err != nil {
		log.Fatal(err)
	}

	list := make([]ExchangeModel.OrderTrade, 0)

	for res.Next() {
		var orderTrade ExchangeModel.OrderTrade
		err := res.Scan(
			&orderTrade.OrderId,
			&orderTrade.Open,
			&orderTrade.Close,
			&orderTrade.Buy,
			&orderTrade.Sell,
			&orderTrade.BuyQuantity,
			&orderTrade.SellQuantity,
			&orderTrade.Profit,
			&orderTrade.Symbol,
			&orderTrade.HoursOpened,
			&orderTrade.Budget,
			&orderTrade.Percent,
		)

		if err != nil {
			log.Fatal(err)
		}

		list = append(list, orderTrade)
	}

	return list
}

func (repo *OrderRepository) GetList() []ExchangeModel.Order {
	res, err := repo.DB.Query(`
		SELECT
		    o.id as Id, 
			o.symbol as Symbol, 
			o.quantity as Quantity,
			o.executed_quantity as ExecutedQuantity,
			o.price as Price,
			o.created_at as CreatedAt,
			o.operation as Operation,
			o.status as Status,
			o.sell_volume as SellVolume,
			o.buy_volume as BuyVolume,
			o.sma_value as SmaValue,
			o.external_id as ExternalId,
			o.closes_order as ClosesOrder,
			o.used_extra_budget as UsedExtraBudget,
			o.commission as Commission,
			o.commission_asset as CommissionAsset,
			SUM(IFNULL(sell.executed_quantity, 0)) as SoldQuantity,
			o.swap as Swap,
			o.extra_charge_options as ExtraChargeOptions
		FROM orders o 
		LEFT JOIN orders sell ON o.id = sell.closes_order AND sell.operation = 'SELL'
		WHERE o.bot_id = ?
		GROUP BY o.id
	`, repo.CurrentBot.Id)
	defer res.Close()

	if err != nil {
		log.Fatal(err)
	}

	list := make([]ExchangeModel.Order, 0)

	for res.Next() {
		var order ExchangeModel.Order
		err := res.Scan(
			&order.Id,
			&order.Symbol,
			&order.Quantity,
			&order.ExecutedQuantity,
			&order.Price,
			&order.CreatedAt,
			&order.Operation,
			&order.Status,
			&order.SellVolume,
			&order.BuyVolume,
			&order.SmaValue,
			&order.ExternalId,
			&order.ClosesOrder,
			&order.UsedExtraBudget,
			&order.Commission,
			&order.CommissionAsset,
			&order.SoldQuantity,
			&order.Swap,
			&order.ExtraChargeOptions,
		)

		if err != nil {
			log.Fatal(err)
		}

		list = append(list, order)
	}

	return list
}

func (repo *OrderRepository) GetClosesOrderList(buyOrder ExchangeModel.Order) []ExchangeModel.Order {
	res, err := repo.DB.Query(`
		SELECT
		    o.id as Id, 
			o.symbol as Symbol, 
			o.quantity as Quantity,
			o.executed_quantity as ExecutedQuantity,
			o.price as Price,
			o.created_at as CreatedAt,
			o.operation as Operation,
			o.status as Status,
			o.sell_volume as SellVolume,
			o.buy_volume as BuyVolume,
			o.sma_value as SmaValue,
			o.external_id as ExternalId,
			o.closes_order as ClosesOrder,
			o.used_extra_budget as UsedExtraBudget,
			o.commission as Commission,
			o.commission_asset as CommissionAsset,
			SUM(IFNULL(sell.executed_quantity, 0)) as SoldQuantity,
			o.swap as Swap,
			o.extra_charge_options as ExtraChargeOptions
		FROM orders o 
		LEFT JOIN orders sell ON o.id = sell.closes_order AND sell.operation = 'SELL'
		WHERE o.bot_id = ? AND o.closes_order = ? AND o.operation = ?
		GROUP BY o.id
	`, repo.CurrentBot.Id, buyOrder.Id, "SELL")
	defer res.Close()

	if err != nil {
		log.Fatal(err)
	}

	list := make([]ExchangeModel.Order, 0)

	for res.Next() {
		var order ExchangeModel.Order
		err := res.Scan(
			&order.Id,
			&order.Symbol,
			&order.Quantity,
			&order.ExecutedQuantity,
			&order.Price,
			&order.CreatedAt,
			&order.Operation,
			&order.Status,
			&order.SellVolume,
			&order.BuyVolume,
			&order.SmaValue,
			&order.ExternalId,
			&order.ClosesOrder,
			&order.UsedExtraBudget,
			&order.Commission,
			&order.CommissionAsset,
			&order.SoldQuantity,
			&order.Swap,
			&order.ExtraChargeOptions,
		)

		if err != nil {
			log.Fatal(err)
		}

		list = append(list, order)
	}

	return list
}

func (repo *OrderRepository) SetBinanceOrder(order ExchangeModel.BinanceOrder) {
	encoded, _ := json.Marshal(order)
	repo.RDB.Set(*repo.Ctx, fmt.Sprintf(
		"binance-order-%s-%s-bot-%d",
		order.Symbol,
		strings.ToLower(order.Side),
		repo.CurrentBot.Id,
	), string(encoded), time.Hour*24*90)
}

func (repo *OrderRepository) GetBinanceOrder(symbol string, operation string) *ExchangeModel.BinanceOrder {
	res := repo.RDB.Get(*repo.Ctx, fmt.Sprintf(
		"binance-order-%s-%s-bot-%d",
		symbol,
		strings.ToLower(operation),
		repo.CurrentBot.Id,
	)).Val()
	if len(res) == 0 {
		return nil
	}

	var dto ExchangeModel.BinanceOrder
	json.Unmarshal([]byte(res), &dto)

	return &dto
}

func (repo *OrderRepository) DeleteBinanceOrder(order ExchangeModel.BinanceOrder) {
	repo.RDB.Del(*repo.Ctx, fmt.Sprintf(
		"binance-order-%s-%s-bot-%d",
		order.Symbol,
		strings.ToLower(order.Side),
		repo.CurrentBot.Id,
	)).Val()
}

func (repo *OrderRepository) GetManualOrder(symbol string) *ExchangeModel.ManualOrder {
	res := repo.RDB.Get(*repo.Ctx, fmt.Sprintf(
		"manual-order-%s-bot-%d",
		strings.ToLower(symbol),
		repo.CurrentBot.Id,
	)).Val()
	if len(res) == 0 {
		return nil
	}

	var dto ExchangeModel.ManualOrder
	json.Unmarshal([]byte(res), &dto)

	return &dto
}

func (repo *OrderRepository) SetManualOrder(order ExchangeModel.ManualOrder) {
	encoded, _ := json.Marshal(order)
	repo.RDB.Set(*repo.Ctx, fmt.Sprintf(
		"manual-order-%s-bot-%d",
		strings.ToLower(order.Symbol),
		repo.CurrentBot.Id,
	), string(encoded), time.Hour*24)
}

func (repo *OrderRepository) DeleteManualOrder(symbol string) {
	repo.RDB.Del(*repo.Ctx, fmt.Sprintf(
		"manual-order-%s-bot-%d",
		strings.ToLower(symbol),
		repo.CurrentBot.Id,
	)).Val()
}

func (repo *OrderRepository) HasBuyLock(symbol string) bool {
	value := repo.RDB.Get(*repo.Ctx, fmt.Sprintf(
		"buy-lock-%s-bot-%d",
		strings.ToLower(symbol),
		repo.CurrentBot.Id,
	)).Val()

	return len(value) > 0
}

func (repo *OrderRepository) LockBuy(symbol string, seconds int64) {
	repo.RDB.Set(*repo.Ctx, fmt.Sprintf(
		"buy-lock-%s-bot-%d",
		strings.ToLower(symbol),
		repo.CurrentBot.Id,
	), "lock", time.Second*time.Duration(seconds))
}
