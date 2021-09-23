package main

import (
	"os"
	"os/signal"
	"syscall"
	"sync"
	"time"
	"strconv"
	"fmt"
	// "reflect"
	"github.com/huobirdcenter/huobi_golang/config"
	"github.com/huobirdcenter/huobi_golang/pkg/client"
	"github.com/huobirdcenter/huobi_golang/pkg/model/market"
	"github.com/huobirdcenter/huobi_golang/pkg/model/order"
	"github.com/huobirdcenter/huobi_golang/pkg/model/common"
	"github.com/huobirdcenter/huobi_golang/logging/applogger"
  )

const PRICENUM int = 36
const CLOSENUM int = 999
const AMOUNTUSDT int = 9
  
var wg sync.WaitGroup
var wgKline sync.WaitGroup

var symbolBuy = make(map[string]int)
var symbolInfo = make(map[string]common.Symbol)
var symbolAmount = make(map[string]float64)
var symbolEma = make(map[string]Ema)
var symbolEmaTimes = make(map[string]int)

var exit bool

type Kline struct {
	Open float64
	Close float64
	High float64
	Low float64
}

type OrderInfo struct {
	Amount float64
	FilledAmount float64
	FilledFees float64
}

type Ema struct {
	Ema39 	float64
	Ema66 	float64
	Ema93 	float64
	Ema333	float64
	Ema666	float64
	Ema999	float64
}

func main() {
	c := make(chan os.Signal)
	signal.Notify(c, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	go func() {
		for s := range c {
			switch s {
			case syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT:
				exit = true
				applogger.Warn("Program Exit... %s", s)
			default:
				applogger.Warn("other signal %s", s)
			}
		}
	}()

	wg.Add(2)
	go goSymbolInfo()
	go goKline()
	wg.Wait()
}

func goSymbolInfo() {
	symbolInfoSync()
	wg.Done()
}

func symbolInfoSync() {
	client := new(client.CommonClient).Init(config.Host)
	resp, err := client.GetSymbols()
	if err != nil {
		exit = true
		applogger.Error("Error: %s", err)
	} else {
		for _, result := range resp {
			if result.QuoteCurrency != "usdt" {
				continue
			}
			symbolInfo[result.Symbol] = result
		}
	}
}

func goKline() {
	timeTickerChan := time.Tick(time.Second * 1)
    for !exit {
		symbols := []string{"htusdt"};

		num := len(symbols)
		if num != 0 {
			wgKline.Add(num)
			for _, result := range symbols {
				kline(result)
			}
			wgKline.Wait()
		}
        <-timeTickerChan
    }
	wg.Done()
}

func kline(symbol string) {
	client := new(client.MarketClient).Init(config.Host)
	num := CLOSENUM
	init := true
	if _, ok := symbolEma[symbol]; ok {
		num = 1
		init = false
	} else {
		symbolEmaTimes[symbol] = 0
		symbolBuy[symbol] = 0
	}

	optionalRequest := market.GetCandlestickOptionalRequest{Period: market.MIN1, Size: num}
	resp, err := client.GetCandlestick(symbol, optionalRequest)
	if err != nil {
		exit = true
		applogger.Error("Error: %s", err)
	} else {
		var buy = symbolBuy[symbol]

		for _, result := range resp {
			close, _ := result.Close.Float64()

			var emaInfo Ema
			if value, ok := symbolEma[symbol]; ok {
				emaInfo = value
			} else {
				emaInfo = Ema{close, close, close, close, close, close}
			}

			currentEma := Ema{
				2.0 / (39 + 1) * close + (39.0 - 1) / (39 + 1) * emaInfo.Ema39,
				2.0 / (66 + 1) * close + (66.0 - 1) / (66 + 1) * emaInfo.Ema66,
				2.0 / (93 + 1) * close + (93.0 - 1) / (93 + 1) * emaInfo.Ema93,
				2.0 / (333+ 1) * close + (333.0- 1) / (333+ 1) * emaInfo.Ema333,
				2.0 / (666+ 1) * close + (666.0- 1) / (666+ 1) * emaInfo.Ema666,
				2.0 / (999+ 1) * close + (999.0- 1) / (999+ 1) * emaInfo.Ema999}

			symbolEma[symbol] = currentEma

			// if !init && symbolEmaTimes[symbol] >= CLOSENUM {
			if !init && symbolEmaTimes[symbol] >= CLOSENUM {
				// if buy == 0 && currentEma.Ema39 > currentEma.Ema66 && currentEma.Ema66 > currentEma.Ema93 && close > currentEma.Ema999 {
				// if currentEma.Ema39 > currentEma.Ema66 && currentEma.Ema66 > currentEma.Ema93 {
				if currentEma.Ema39 < currentEma.Ema66 && currentEma.Ema66 < currentEma.Ema93 {
					if buy == 1 {
						orderId := sellHandle(symbol, close)
						if orderId != "" {
							orderInfo := orderInfoHandle(orderId)
					
							realAmount := orderInfo.FilledAmount
							if realAmount > 0 {
								amount := 0.0
								if value, ok := symbolAmount[symbol]; ok {
									amount = value - 0.0001
								}
								symbolAmount[symbol], _ = strconv.ParseFloat(fmt.Sprintf("%."+strconv.Itoa(symbolInfo[symbol].AmountPrecision)+"f", amount - realAmount),64)
		
								minAmount, _ := symbolInfo[symbol].LimitOrderMinOrderAmt.Float64()
								if symbolAmount[symbol] <= minAmount {
									buy = 0
								}
							}
						}
					}
				} else if currentEma.Ema333 > currentEma.Ema666 && currentEma.Ema666 > currentEma.Ema999 {
					if buy == 0 {
						orderId := buyHandle(symbol, close)
						if orderId != "" {
							orderInfo := orderInfoHandle(orderId)
					
							realAmount := orderInfo.FilledAmount
							if realAmount > 0 {
								amount := 0.0
								if value, ok := symbolAmount[symbol]; ok {
									amount = value
								}
								symbolAmount[symbol], _ = strconv.ParseFloat(fmt.Sprintf("%."+strconv.Itoa(symbolInfo[symbol].AmountPrecision)+"f", amount + realAmount),64)
								minAmount, _ := symbolInfo[symbol].LimitOrderMinOrderAmt.Float64()
								if symbolAmount[symbol] >= minAmount {
									buy = 1
								}
							}
						}
					}
				// } else if buy == 1 && currentEma.Ema39 < currentEma.Ema66 && currentEma.Ema66 < currentEma.Ema93 && currentEma.Ema93 < currentEma.Ema999 {
				// } else if buy == 1 && close < currentEma.Ema999 && currentEma.Ema39 < currentEma.Ema66 && currentEma.Ema66 < currentEma.Ema93 {
				// } else if currentEma.Ema333 < currentEma.Ema666 && currentEma.Ema666 < currentEma.Ema999 {
				} 
				symbolBuy[symbol] = buy
			}

			if !init {
				symbolEmaTimes[symbol] ++

				applogger.Info("%s: %f %d %d %+v", symbol, close, symbolEmaTimes[symbol], buy, currentEma)
			}
		}
	}
	wgKline.Done()
}

func buyHandle(symbol string, price float64) (string) {
	client := new(client.OrderClient).Init(config.AccessKey, config.SecretKey, config.Host)
	
	priceS := strconv.FormatFloat(price,'E',-1,64)

	amountUsdt := float64(AMOUNTUSDT)
	amount := fmt.Sprintf("%."+strconv.Itoa(symbolInfo[symbol].AmountPrecision)+"f", amountUsdt / price)

	request := order.PlaceOrderRequest{
		AccountId: config.AccountId,
		Type:      "buy-ioc",
		Source:    "spot-api",
		Symbol:    symbol,
		Price:     priceS,
		Amount:    amount,
	}

	resp, err := client.PlaceOrder(&request)
	if err != nil {
		exit = true
		applogger.Error(err.Error())
	} else {
		switch resp.Status {
		case "ok":
			// applogger.Info("Place order successfully, order id: %s", resp.Data)
			return resp.Data
		case "error":
			exit = true
			applogger.Error("Place order error: %s", resp.ErrorMessage)
			// applogger.Info("Place order error: %s", resp.ErrorMessage)
			return ""
		}
	}

	return ""
}

func sellHandle(symbol string, price float64) (string) {
	client := new(client.OrderClient).Init(config.AccessKey, config.SecretKey, config.Host)
	
	priceS := strconv.FormatFloat(price,'E',-1,64)

	amount := symbolAmount[symbol]
	amountS := strconv.FormatFloat(amount,'E',-1,64)

	request := order.PlaceOrderRequest{
		AccountId: config.AccountId,
		Type:      "sell-ioc",
		Source:    "spot-api",
		Symbol:    symbol,
		Price:     priceS,
		Amount:    amountS,
	}

	resp, err := client.PlaceOrder(&request)
	if err != nil {
		exit = true
		applogger.Error(err.Error())
	} else {
		switch resp.Status {
		case "ok":
			// applogger.Info("Place order successfully, order id: %s", resp.Data)
			return resp.Data
		case "error":
			exit = true
			applogger.Error("Place order error: %s", resp.ErrorMessage)
			// applogger.Info("Place order error: %s", resp.ErrorMessage)
			return ""
		}
	}

	return ""
}

func orderInfoHandle(orderId string) (OrderInfo) {
	client := new(client.OrderClient).Init(config.AccessKey, config.SecretKey, config.Host)
	resp, err := client.GetOrderById(orderId)
	if err != nil {
		applogger.Error(err.Error())
	} else {
		switch resp.Status {
		case "ok":
			if resp.Data != nil {
				o := resp.Data
				applogger.Info("Get order, symbol: %s, price: %s, amount: %s, filled amount: %s, filled cash amount: %s, filled fees: %s",
					o.Symbol, o.Price, o.Amount, o.FilledAmount, o.FilledCashAmount, o.FilledFees)
					
				amount, _ := strconv.ParseFloat(resp.Data.Amount,64)
				filledAmount, _ := strconv.ParseFloat(resp.Data.FilledAmount,64)
				filledFees, _ := strconv.ParseFloat(resp.Data.FilledFees,64)
				return OrderInfo{amount, filledAmount, filledFees}
			}
		case "error":
			applogger.Error("Get order by id error: %s", resp.ErrorMessage)
		}
	}

	return OrderInfo{}
}