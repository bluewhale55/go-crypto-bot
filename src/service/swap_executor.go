package service

import (
	"errors"
	"fmt"
	"gitlab.com/open-soft/go-crypto-bot/src/client"
	ExchangeModel "gitlab.com/open-soft/go-crypto-bot/src/model"
	"gitlab.com/open-soft/go-crypto-bot/src/repository"
	"log"
	"strings"
	"time"
)

type SwapExecutorInterface interface {
	Execute(order ExchangeModel.Order)
}

type SwapExecutor struct {
	SwapRepository  repository.SwapBasicRepositoryInterface
	OrderRepository repository.OrderUpdaterInterface
	BalanceService  BalanceServiceInterface
	Binance         client.ExchangeOrderAPIInterface
	TimeService     TimeServiceInterface
	Formatter       *Formatter
}

func (s *SwapExecutor) Execute(order ExchangeModel.Order) {
	swapAction, err := s.SwapRepository.GetActiveSwapAction(order)

	if err != nil {
		log.Printf("[%s] Swap processing error: %s", order.Symbol, err.Error())

		if strings.Contains(err.Error(), "no rows in result set") {
			order.Swap = false
			_ = s.OrderRepository.Update(order)
		}

		return
	}

	balanceBefore, _ := s.BalanceService.GetAssetBalance(swapAction.Asset, false)

	if swapAction.IsPending() {
		swapAction.Status = ExchangeModel.SwapActionStatusProcess
		_ = s.SwapRepository.UpdateSwapAction(swapAction)
	}

	swapChain, err := s.SwapRepository.GetSwapChainById(swapAction.SwapChainId)

	if err != nil {
		log.Printf("Swap chain %d is not found", swapAction.SwapChainId)
		return
	}

	swapOneOrder := s.ExecuteSwapOne(&swapAction, order)

	if swapOneOrder == nil {
		return
	}

	swapTwoOrder := s.ExecuteSwapTwo(&swapAction, swapChain, *swapOneOrder)

	if swapTwoOrder == nil {
		return
	}

	assetTwo := strings.ReplaceAll(swapOneOrder.Symbol, swapAction.Asset, "")

	// step 3
	swapThreeOrder := s.ExecuteSwapThree(&swapAction, swapChain, *swapTwoOrder, assetTwo)

	if swapThreeOrder == nil {
		return
	}

	endQuantity := swapThreeOrder.ExecutedQty
	if swapChain.IsSBS() {
		endQuantity = swapThreeOrder.CummulativeQuoteQty
	}

	order.Swap = false
	swapAction.Status = ExchangeModel.SwapActionStatusSuccess
	nowTimestamp := time.Now().Unix()
	swapAction.EndTimestamp = &nowTimestamp
	swapAction.SwapThreeTimestamp = &nowTimestamp
	swapAction.EndQuantity = &endQuantity
	_ = s.SwapRepository.UpdateSwapAction(swapAction)
	_ = s.OrderRepository.Update(order)

	s.BalanceService.InvalidateBalanceCache(swapAction.Asset)
	balanceAfter, _ := s.BalanceService.GetAssetBalance(swapAction.Asset, false)

	log.Printf(
		"[%s] Swap funished, balance %s before = %f after = %f",
		order.Symbol,
		swapAction.Asset,
		balanceBefore,
		balanceAfter,
	)
}

func (s *SwapExecutor) ExecuteSwapOne(swapAction *ExchangeModel.SwapAction, order ExchangeModel.Order) *ExchangeModel.BinanceOrder {
	var swapOneOrder *ExchangeModel.BinanceOrder = nil

	if swapAction.SwapOneExternalId == nil {
		swapPair, err := s.SwapRepository.GetSwapPairBySymbol(swapAction.SwapOneSymbol)
		binanceOrder, err := s.Binance.LimitOrder(
			swapAction.SwapOneSymbol,
			s.Formatter.FormatQuantity(swapPair, swapAction.StartQuantity),
			s.Formatter.FormatPrice(swapPair, swapAction.SwapOnePrice),
			"SELL",
			"GTC",
		)

		if err != nil {
			log.Printf(
				"[%s] Swap [%d] error: %s",
				order.Symbol,
				swapAction.Id,
				err.Error(),
			)

			orderStatus := "ERROR"
			swapAction.SwapOneExternalStatus = &orderStatus
			swapAction.Status = ExchangeModel.SwapActionStatusCanceled
			nowTimestamp := time.Now().Unix()
			swapAction.EndTimestamp = &nowTimestamp
			swapAction.EndQuantity = &swapAction.StartQuantity
			_ = s.SwapRepository.UpdateSwapAction(*swapAction)
			order.Swap = false
			_ = s.OrderRepository.Update(order)
			// invalidate balance cache
			s.BalanceService.InvalidateBalanceCache(swapAction.Asset)
			return nil
		}

		swapOneOrder = &binanceOrder
		swapAction.SwapOneExternalId = &binanceOrder.OrderId
		nowTimestamp := time.Now().Unix()
		swapAction.SwapOneTimestamp = &nowTimestamp
		swapAction.SwapOneExternalStatus = &binanceOrder.Status
		_ = s.SwapRepository.UpdateSwapAction(*swapAction)
	} else {
		binanceOrder, err := s.Binance.QueryOrder(swapAction.SwapOneSymbol, *swapAction.SwapOneExternalId)
		if err != nil {
			log.Printf("[%s] Swap error: %s", order.Symbol, err.Error())
			return nil
		}

		if binanceOrder.IsCanceled() || binanceOrder.IsExpired() {
			swapAction.SwapOneExternalId = nil
			swapAction.SwapOneTimestamp = nil
			swapAction.SwapOneExternalStatus = nil
			_ = s.SwapRepository.UpdateSwapAction(*swapAction)
			s.BalanceService.InvalidateBalanceCache(swapAction.Asset)

			return nil
		}

		swapOneOrder = &binanceOrder
		swapAction.SwapOneExternalStatus = &binanceOrder.Status
		_ = s.SwapRepository.UpdateSwapAction(*swapAction)
	}

	// step 1

	// todo: if expired, clear and call recursively
	if !swapOneOrder.IsFilled() {
		s.TimeService.WaitSeconds(5)
		for {
			binanceOrder, err := s.Binance.QueryOrder(swapOneOrder.Symbol, swapOneOrder.OrderId)
			if err != nil {
				log.Printf("[%s] Swap Binance error: %s", order.Symbol, err.Error())

				continue
			}

			swapPair, err := s.SwapRepository.GetSwapPairBySymbol(binanceOrder.Symbol)

			log.Printf(
				"[%s] Swap [%d] one [%s] processing, status %s [%d], price %f, current = %f, Executed %f of %f",
				swapAction.SwapOneSymbol,
				swapAction.Id,
				binanceOrder.Side,
				binanceOrder.Status,
				binanceOrder.OrderId,
				binanceOrder.Price,
				swapPair.SellPrice,
				binanceOrder.ExecutedQty,
				binanceOrder.OrigQty,
			)

			// update value, set new memory address
			swapOneOrder = &binanceOrder

			nowTimestamp := time.Now().Unix()
			swapAction.SwapOneTimestamp = &nowTimestamp
			swapAction.SwapOneExternalStatus = &binanceOrder.Status
			_ = s.SwapRepository.UpdateSwapAction(*swapAction)

			if binanceOrder.IsFilled() {
				break
			}

			// todo: timeout... cancel and remove swap action...

			if binanceOrder.IsCanceled() || binanceOrder.IsExpired() {
				swapAction.SwapOneExternalStatus = &binanceOrder.Status
				swapAction.Status = ExchangeModel.SwapActionStatusCanceled
				nowTimestamp := time.Now().Unix()
				swapAction.EndTimestamp = &nowTimestamp
				swapAction.EndQuantity = &swapAction.StartQuantity
				_ = s.SwapRepository.UpdateSwapAction(*swapAction)
				order.Swap = false
				_ = s.OrderRepository.Update(order)
				// invalidate balance cache
				s.BalanceService.InvalidateBalanceCache(swapAction.Asset)
				log.Printf("[%s] Swap one process cancelled, cancel all the operation!", order.Symbol)

				return nil
			}

			// cancel if we can not start processing more than 1 minute
			if binanceOrder.IsNew() && s.TimeService.GetNowDiffMinutes(swapAction.StartTimestamp) >= 1 {
				cancelOrder, err := s.Binance.CancelOrder(binanceOrder.Symbol, binanceOrder.OrderId)
				if err == nil {
					swapAction.SwapOneExternalStatus = &cancelOrder.Status
					swapAction.Status = ExchangeModel.SwapActionStatusCanceled
					nowTimestamp := time.Now().Unix()
					swapAction.EndTimestamp = &nowTimestamp
					swapAction.EndQuantity = &swapAction.StartQuantity
					_ = s.SwapRepository.UpdateSwapAction(*swapAction)
					order.Swap = false
					_ = s.OrderRepository.Update(order)
					// invalidate balance cache
					s.BalanceService.InvalidateBalanceCache(swapAction.Asset)
					log.Printf("[%s] Swap process cancelled, couldn't be processed more than 60 seconds", order.Symbol)

					return nil
				}
			}

			if binanceOrder.IsPartiallyFilled() {
				s.TimeService.WaitSeconds(7)
			} else {
				s.TimeService.WaitSeconds(15)
			}
		}
	}

	return swapOneOrder
}

func (s *SwapExecutor) ExecuteSwapTwo(
	swapAction *ExchangeModel.SwapAction,
	swapChain ExchangeModel.SwapChainEntity,
	swapOneOrder ExchangeModel.BinanceOrder,
) *ExchangeModel.BinanceOrder {
	assetTwo := strings.ReplaceAll(swapOneOrder.Symbol, swapAction.Asset, "")

	var swapTwoOrder *ExchangeModel.BinanceOrder = nil

	if swapAction.SwapTwoExternalId == nil {
		balance, _ := s.BalanceService.GetAssetBalance(assetTwo, false)
		// Calculate how much we earn, and sell it!
		quantity := swapOneOrder.CummulativeQuoteQty

		if quantity > balance {
			quantity = balance
		}

		log.Printf(
			"[%s] Swap [%d] two balance %s is %f, operation SELL %s",
			swapChain.SwapTwo.Symbol,
			swapAction.Id,
			assetTwo,
			balance,
			swapAction.SwapTwoSymbol,
		)

		swapPair, err := s.SwapRepository.GetSwapPairBySymbol(swapAction.SwapTwoSymbol)
		var binanceOrder ExchangeModel.BinanceOrder

		if swapChain.IsSSB() {
			binanceOrder, err = s.Binance.LimitOrder(
				swapAction.SwapTwoSymbol,
				s.Formatter.FormatQuantity(swapPair, quantity),
				s.Formatter.FormatPrice(swapPair, swapAction.SwapTwoPrice),
				"SELL",
				"GTC",
			)
		}

		if swapChain.IsSBB() || swapChain.IsSBS() {
			binanceOrder, err = s.Binance.LimitOrder(
				swapAction.SwapTwoSymbol,
				s.Formatter.FormatQuantity(swapPair, quantity/swapAction.SwapTwoPrice),
				s.Formatter.FormatPrice(swapPair, swapAction.SwapTwoPrice),
				"BUY",
				"GTC",
			)
		}

		if err != nil {
			log.Printf(
				"[%s] Swap [%d] error: %s",
				swapAction.SwapTwoSymbol,
				swapAction.Id,
				err.Error(),
			)
			return nil
		}

		swapTwoOrder = &binanceOrder
		swapAction.SwapTwoExternalId = &binanceOrder.OrderId
		nowTimestamp := time.Now().Unix()
		swapAction.SwapTwoTimestamp = &nowTimestamp
		swapAction.SwapTwoExternalStatus = &binanceOrder.Status
		_ = s.SwapRepository.UpdateSwapAction(*swapAction)
	} else {
		binanceOrder, err := s.Binance.QueryOrder(swapAction.SwapTwoSymbol, *swapAction.SwapTwoExternalId)
		if err != nil {
			log.Printf("[%s] Swap error: %s", swapAction.SwapTwoSymbol, err.Error())
			return nil
		}

		if binanceOrder.IsCanceled() || binanceOrder.IsExpired() {
			swapAction.SwapTwoExternalId = nil
			swapAction.SwapTwoTimestamp = nil
			swapAction.SwapTwoExternalStatus = nil
			_ = s.SwapRepository.UpdateSwapAction(*swapAction)
			s.BalanceService.InvalidateBalanceCache(assetTwo)

			return nil
		}

		swapTwoOrder = &binanceOrder
		swapAction.SwapTwoExternalStatus = &binanceOrder.Status
		_ = s.SwapRepository.UpdateSwapAction(*swapAction)
	}

	if !swapTwoOrder.IsFilled() {
		s.TimeService.WaitSeconds(5)
		for {
			binanceOrder, err := s.Binance.QueryOrder(swapTwoOrder.Symbol, swapTwoOrder.OrderId)
			if err != nil {
				log.Printf("[%s] Swap Binance error: %s", swapAction.SwapTwoSymbol, err.Error())

				continue
			}

			swapPair, err := s.SwapRepository.GetSwapPairBySymbol(binanceOrder.Symbol)

			log.Printf(
				"[%s] Swap [%d] two [%s] processing, status %s [%d], price %f, current = %f, Executed %f of %f",
				swapAction.SwapTwoSymbol,
				swapAction.Id,
				binanceOrder.Side,
				binanceOrder.Status,
				binanceOrder.OrderId,
				binanceOrder.Price,
				swapPair.SellPrice,
				binanceOrder.ExecutedQty,
				binanceOrder.OrigQty,
			)

			// update value, set new memory address
			swapTwoOrder = &binanceOrder

			nowTimestamp := time.Now().Unix()
			swapAction.SwapTwoTimestamp = &nowTimestamp
			swapAction.SwapTwoExternalStatus = &binanceOrder.Status
			_ = s.SwapRepository.UpdateSwapAction(*swapAction)

			if binanceOrder.IsFilled() {
				break
			}

			if binanceOrder.IsCanceled() || binanceOrder.IsExpired() {
				swapAction.SwapTwoExternalId = nil
				swapAction.SwapTwoTimestamp = nil
				swapAction.SwapTwoExternalStatus = nil
				_ = s.SwapRepository.UpdateSwapAction(*swapAction)
				s.BalanceService.InvalidateBalanceCache(assetTwo)

				return nil
			}

			// 2 hours can not process second step
			if binanceOrder.IsNew() && s.TimeService.GetNowDiffMinutes(*swapAction.SwapOneTimestamp) > 5 {
				// todo: rollback savepoint!!!
				err = s.TryRollbackSwapTwo(swapAction, swapChain, swapOneOrder, assetTwo)
				if err == nil {
					return nil
				}

				log.Printf("Swap two [%d] rollback: %s", swapAction.Id, err.Error())
			}

			if binanceOrder.IsPartiallyFilled() {
				s.TimeService.WaitSeconds(7)
			} else {
				s.TimeService.WaitSeconds(15)
			}
		}
	}

	return swapTwoOrder
}

func (s *SwapExecutor) ExecuteSwapThree(
	swapAction *ExchangeModel.SwapAction,
	swapChain ExchangeModel.SwapChainEntity,
	swapTwoOrder ExchangeModel.BinanceOrder,
	assetTwo string,
) *ExchangeModel.BinanceOrder {
	assetThree := strings.ReplaceAll(swapTwoOrder.Symbol, assetTwo, "")
	var swapThreeOrder *ExchangeModel.BinanceOrder = nil

	if swapAction.SwapThreeExternalId == nil {
		balance, _ := s.BalanceService.GetAssetBalance(assetThree, false)
		quantity := swapTwoOrder.CummulativeQuoteQty

		if swapChain.IsSBS() || swapChain.IsSBB() {
			quantity = swapTwoOrder.ExecutedQty
		}

		if quantity > balance {
			quantity = balance
		}

		log.Printf(
			"[%s] Swap [%d] three balance %s is %f, operation BUY %s",
			swapChain.SwapThree.Symbol,
			swapAction.Id,
			assetThree,
			balance,
			swapAction.SwapThreeSymbol,
		)

		swapPair, err := s.SwapRepository.GetSwapPairBySymbol(swapAction.SwapThreeSymbol)
		var binanceOrder ExchangeModel.BinanceOrder

		if swapChain.IsSSB() || swapChain.IsSBB() {
			binanceOrder, err = s.Binance.LimitOrder(
				swapAction.SwapThreeSymbol,
				s.Formatter.FormatQuantity(swapPair, quantity/swapAction.SwapThreePrice),
				s.Formatter.FormatPrice(swapPair, swapAction.SwapThreePrice),
				"BUY",
				"GTC",
			)
		}

		if swapChain.IsSBS() {
			binanceOrder, err = s.Binance.LimitOrder(
				swapAction.SwapThreeSymbol,
				s.Formatter.FormatQuantity(swapPair, quantity),
				s.Formatter.FormatPrice(swapPair, swapAction.SwapThreePrice),
				"SELL",
				"GTC",
			)
		}

		if err != nil {
			log.Printf(
				"[%s] Swap [%d] three error: %s",
				swapChain.SwapThree.Symbol,
				swapAction.Id,
				err.Error(),
			)
			return nil
		}

		swapThreeOrder = &binanceOrder
		swapAction.SwapThreeExternalId = &binanceOrder.OrderId
		nowTimestamp := time.Now().Unix()
		swapAction.SwapThreeTimestamp = &nowTimestamp
		swapAction.SwapThreeExternalStatus = &binanceOrder.Status
		_ = s.SwapRepository.UpdateSwapAction(*swapAction)
	} else {
		binanceOrder, err := s.Binance.QueryOrder(swapAction.SwapThreeSymbol, *swapAction.SwapThreeExternalId)
		if err != nil {
			log.Printf("[%s] Swap error: %s", swapChain.SwapThree.Symbol, err.Error())
			return nil
		}

		if binanceOrder.IsCanceled() || binanceOrder.IsExpired() {
			swapAction.SwapThreeExternalId = nil
			swapAction.SwapThreeTimestamp = nil
			swapAction.SwapThreeExternalStatus = nil
			_ = s.SwapRepository.UpdateSwapAction(*swapAction)
			s.BalanceService.InvalidateBalanceCache(assetThree)

			return nil
		}

		swapThreeOrder = &binanceOrder
		swapAction.SwapThreeExternalStatus = &binanceOrder.Status
		_ = s.SwapRepository.UpdateSwapAction(*swapAction)
	}

	// todo: if expired, clear and call recursively
	if !swapThreeOrder.IsFilled() {
		s.TimeService.WaitSeconds(5)
		for {
			binanceOrder, err := s.Binance.QueryOrder(swapThreeOrder.Symbol, swapThreeOrder.OrderId)
			if err != nil {
				log.Printf("[%s] Swap Binance error: %s", swapChain.SwapThree.Symbol, err.Error())

				continue
			}

			swapPair, err := s.SwapRepository.GetSwapPairBySymbol(binanceOrder.Symbol)

			log.Printf(
				"[%s] Swap [%d] three [%s] processing, status %s [%d], price %f, current = %f, Executed %f of %f",
				swapAction.SwapThreeSymbol,
				swapAction.Id,
				binanceOrder.Side,
				binanceOrder.Status,
				binanceOrder.OrderId,
				binanceOrder.Price,
				swapPair.BuyPrice,
				binanceOrder.ExecutedQty,
				binanceOrder.OrigQty,
			)

			// update value, set new memory address
			swapThreeOrder = &binanceOrder

			nowTimestamp := time.Now().Unix()
			swapAction.SwapThreeTimestamp = &nowTimestamp
			swapAction.SwapThreeExternalStatus = &binanceOrder.Status
			_ = s.SwapRepository.UpdateSwapAction(*swapAction)

			if binanceOrder.IsFilled() {
				break
			}

			if binanceOrder.IsCanceled() || binanceOrder.IsExpired() {
				swapAction.SwapThreeExternalId = nil
				swapAction.SwapThreeTimestamp = nil
				swapAction.SwapThreeExternalStatus = nil
				_ = s.SwapRepository.UpdateSwapAction(*swapAction)
				s.BalanceService.InvalidateBalanceCache(assetThree)

				return nil
			}

			currentPrice := swapPair.BuyPrice
			priceDeadlineReached := false
			if swapChain.IsSBS() {
				currentPrice = swapPair.SellPrice
			}
			priceDiff := s.Formatter.ComparePercentage(binanceOrder.Price, currentPrice) - 100.00

			// todo: half of minimum swap percent
			if (swapChain.IsSSB() || swapChain.IsSBB()) && priceDiff.Gte(0.15) {
				priceDeadlineReached = true
			}
			if swapChain.IsSBS() && priceDiff.Lte(-0.15) {
				priceDeadlineReached = true
			}

			// 2 hours can not process third step
			if binanceOrder.IsNew() && (s.TimeService.GetNowDiffMinutes(*swapAction.SwapTwoTimestamp) > 10 || priceDeadlineReached) {
				// todo: force swap savepoint!!!
				err = s.TryForceSwapThree(swapAction, swapChain, swapTwoOrder, assetThree)
				if err == nil {
					// rolled back successfully!
					return nil
				}

				log.Printf("Swap three [%d] force swap: %s", swapAction.Id, err.Error())
			}

			if binanceOrder.IsPartiallyFilled() {
				swapAction.EndQuantity = &binanceOrder.ExecutedQty
				_ = s.SwapRepository.UpdateSwapAction(*swapAction)

				if (nowTimestamp-swapAction.StartTimestamp) > (3600*4) && binanceOrder.IsNearlyFilled() {
					break // Do not cancel order, but check it later...
				}
				s.TimeService.WaitSeconds(7)
			} else {
				s.TimeService.WaitSeconds(15)
			}
		}
	}

	return swapThreeOrder
}

func (s *SwapExecutor) TryRollbackSwapTwo(
	action *ExchangeModel.SwapAction,
	swapChain ExchangeModel.SwapChainEntity,
	swapOneOrder ExchangeModel.BinanceOrder,
	asset string,
) error {
	if !swapChain.IsSSB() && !swapChain.IsSBS() && !swapChain.IsSBB() {
		return errors.New("Swap chain type is not supported")
	}

	minSwapRollbackPercent := ExchangeModel.Percent(0.75)

	// pre-validate rollback...
	swapPair, err := s.SwapRepository.GetSwapPairBySymbol(action.SwapOneSymbol)
	if err != nil {
		return err
	}

	price := swapPair.BuyPrice + swapPair.MinPrice

	endQuantity := s.Formatter.FormatQuantity(swapPair, swapOneOrder.CummulativeQuoteQty/price)
	percent := s.Formatter.ComparePercentage(action.StartQuantity, endQuantity) - 100.00

	if percent.Lt(minSwapRollbackPercent) {
		return errors.New(fmt.Sprintf("Is not possible to rollback: %f -> %f", action.StartQuantity, endQuantity))
	}

	balance, err := s.BalanceService.GetAssetBalance(asset, false)

	if err != nil {
		return err
	}

	_, err = s.Binance.CancelOrder(action.SwapTwoSymbol, *action.SwapTwoExternalId)
	if err != nil {
		return err
	}

	for i := 1.00; i <= 100.00; i++ {
		swapPair, err = s.SwapRepository.GetSwapPairBySymbol(action.SwapOneSymbol)

		if err != nil {
			return err
		}

		price = swapPair.BuyPrice + (swapPair.MinPrice * i)
		quantity := swapOneOrder.CummulativeQuoteQty

		if quantity > balance {
			quantity = balance
		}

		if quantity < swapPair.MinNotional {
			return errors.New("Notional filter")
		}

		endQuantity = s.Formatter.FormatQuantity(swapPair, quantity/price)
		percent = s.Formatter.ComparePercentage(action.StartQuantity, endQuantity) - 100.00

		if percent.Gte(minSwapRollbackPercent) {
			binanceOrder, err := s.Binance.LimitOrder(
				swapOneOrder.Symbol,
				endQuantity,
				s.Formatter.FormatPrice(swapPair, price),
				"BUY",
				"IOC",
			)
			if err != nil {
				return err
			}

			if !binanceOrder.IsFilled() {
				log.Printf(
					"Can not fill rollback order, status: %s | price: %f, current: %f [%.2f%s] %f -> %f",
					binanceOrder.Status,
					binanceOrder.Price,
					swapPair.BuyPrice,
					percent,
					"%",
					action.StartQuantity,
					endQuantity,
				)
				s.TimeService.WaitSeconds(5)
				continue
			}

			// save information about rollback transaction...
			action.EndQuantity = &binanceOrder.ExecutedQty
			now := time.Now().Unix()
			action.EndTimestamp = &now
			status := fmt.Sprintf("%s_RB", binanceOrder.Status)
			action.SwapTwoTimestamp = &now
			action.SwapTwoExternalStatus = &status
			action.SwapTwoPrice = binanceOrder.Price
			action.SwapTwoSymbol = binanceOrder.Symbol
			action.SwapTwoExternalId = &binanceOrder.OrderId
			action.Status = ExchangeModel.SwapActionStatusSuccess
			err = s.SwapRepository.UpdateSwapAction(*action)
			if err != nil {
				panic(err)
			}
			return nil
		} else {
			return errors.New(fmt.Sprintf("Can't rollback swap, percent is too low: %.2f%s", percent, "%"))
		}
	}

	return errors.New("Can't rollback swap, all attempts are finished")
}

func (s *SwapExecutor) TryForceSwapThree(
	swapAction *ExchangeModel.SwapAction,
	swapChain ExchangeModel.SwapChainEntity,
	swapTwoOrder ExchangeModel.BinanceOrder,
	asset string,
) error {
	if !swapChain.IsSSB() && !swapChain.IsSBS() && !swapChain.IsSBB() {
		return errors.New("Swap chain type is not supported")
	}

	minSwapRollbackPercent := ExchangeModel.Percent(0.75)

	// pre-validate rollback...
	swapPair, err := s.SwapRepository.GetSwapPairBySymbol(swapAction.SwapThreeSymbol)
	if err != nil {
		return err
	}

	price := swapPair.BuyPrice + swapPair.MinPrice
	if swapChain.IsSBS() {
		price = swapPair.SellPrice - swapPair.MinPrice
	}

	quantity := swapTwoOrder.CummulativeQuoteQty

	if swapChain.IsSBS() || swapChain.IsSBB() {
		quantity = swapTwoOrder.ExecutedQty
	}

	endQuantity := quantity * price
	if swapChain.IsSSB() || swapChain.IsSBB() {
		endQuantity = quantity / price
	}

	if endQuantity == 0.00 {
		return errors.New("Incorrect swap calculation")
	}

	percent := s.Formatter.ComparePercentage(swapAction.StartQuantity, endQuantity) - 100.00

	if percent.Lt(minSwapRollbackPercent) {
		return errors.New(fmt.Sprintf(
			"[%s] Is not possible to force swap: %f -> %f",
			swapChain.Type,
			swapAction.StartQuantity,
			endQuantity,
		))
	}

	_, err = s.Binance.CancelOrder(swapAction.SwapThreeSymbol, *swapAction.SwapThreeExternalId)
	if err != nil {
		return err
	}

	balance, err := s.BalanceService.GetAssetBalance(asset, false)

	if err != nil {
		return err
	}

	if quantity > balance {
		quantity = balance
	}

	for i := 1.00; i <= 100.00; i++ {
		swapPair, err = s.SwapRepository.GetSwapPairBySymbol(swapAction.SwapThreeSymbol)

		if err != nil {
			return err
		}

		price = swapPair.BuyPrice + (swapPair.MinPrice * i)

		if swapChain.IsSBS() {
			price = swapPair.SellPrice - (swapPair.MinPrice * i)
		}

		predictedEndQty := quantity / price
		if swapChain.IsSBS() {
			predictedEndQty = quantity
		}

		percent = s.Formatter.ComparePercentage(swapAction.StartQuantity, predictedEndQty) - 100.00

		if percent.Gte(minSwapRollbackPercent) {
			var binanceOrder ExchangeModel.BinanceOrder

			// todo: find required quantity in order book

			if swapChain.IsSSB() || swapChain.IsSBB() {
				binanceOrder, err = s.Binance.LimitOrder(
					swapAction.SwapThreeSymbol,
					s.Formatter.FormatQuantity(swapPair, quantity/price),
					s.Formatter.FormatPrice(swapPair, price),
					"BUY",
					"IOC",
				)
			}

			if swapChain.IsSBS() {
				binanceOrder, err = s.Binance.LimitOrder(
					swapAction.SwapThreeSymbol,
					s.Formatter.FormatQuantity(swapPair, quantity),
					s.Formatter.FormatPrice(swapPair, price),
					"SELL",
					"IOC",
				)
			}

			if !binanceOrder.IsFilled() {
				log.Printf(
					"Can not fill force swap order, status: %s | price: %f, current: %f [%.2f%s] %f -> %f",
					binanceOrder.Status,
					binanceOrder.Price,
					swapPair.BuyPrice,
					percent,
					"%",
					swapAction.StartQuantity,
					endQuantity,
				)
				s.TimeService.WaitSeconds(5)
				continue
			}

			// save information about rollback transaction...
			swapAction.EndQuantity = &binanceOrder.ExecutedQty
			if swapChain.IsSBS() {
				swapAction.EndQuantity = &binanceOrder.CummulativeQuoteQty
			}
			now := time.Now().Unix()
			swapAction.EndTimestamp = &now
			status := fmt.Sprintf("%s_FORCE", binanceOrder.Status)
			swapAction.SwapThreeTimestamp = &now
			swapAction.SwapThreeExternalStatus = &status
			swapAction.SwapThreePrice = binanceOrder.Price
			swapAction.SwapThreeSymbol = binanceOrder.Symbol
			swapAction.SwapThreeExternalId = &binanceOrder.OrderId
			swapAction.Status = ExchangeModel.SwapActionStatusSuccess
			err = s.SwapRepository.UpdateSwapAction(*swapAction)
			if err != nil {
				panic(err)
			}
			return nil
		} else {
			return errors.New(fmt.Sprintf("Can't force swap, percent is too low: %.2f%s", percent, "%"))
		}
	}

	return errors.New("Can't force swap")
}
