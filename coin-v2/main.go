package main

import (
	"os"
	"os/signal"
	"syscall"
	"sync"
	"time"
	"strconv"
	"fmt"
	"regexp"
	"github.com/huobirdcenter/huobi_golang/config"
	"github.com/huobirdcenter/huobi_golang/pkg/client"
	"github.com/huobirdcenter/huobi_golang/pkg/model/market"
	"github.com/huobirdcenter/huobi_golang/pkg/model/order"
	"github.com/huobirdcenter/huobi_golang/pkg/model/common"
	"github.com/huobirdcenter/huobi_golang/logging/applogger"
)

const DEALNUM int = 9
  
var wg sync.WaitGroup
var wgMax9day sync.WaitGroup

var symbolInfo = make(map[string]common.Symbol)
var symbolsLow = make(map[string]float64)
var symbolsHigh = make(map[string]float64)
var symbolsPrice = make(map[string]float64)
var max9daySymbols []string
var currencysBalance = make(map[string]float64)
var MarketClient *client.MarketClient

var exit bool

type OrderInfo struct {
	Amount float64
	FilledAmount float64
	FilledFees float64
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

	initClient()
	balanceSync()
	infoSync()
	max9day()

	wg.Add(2)
	go goMax9Day()
	go goHandle()
	wg.Wait()
}

func initClient() {
	MarketClient = new(client.MarketClient).Init(config.Host)
}

func goMax9Day() {
	timeTickerChan := time.Tick(999999999)
    for !exit {
		max9day()
        <-timeTickerChan
    }

	wg.Done()
}

func max9day() {
	resp, err := MarketClient.GetAllSymbolsLast24hCandlesticksAskBid()
	if err != nil {
		exit = true
		applogger.Error("max9dayError: %s", err)
	} else {
		symbols := []string{}
		for _, result := range resp {
			if str4 := result.Symbol[len(result.Symbol) - 4 :]; str4 != "usdt" {
				continue
			}

			if result.Vol.IntPart() < 999999 * 9 {
				continue
			}

			regexp, _ := regexp.Compile(`([\d])[l|s]`)
			match := regexp.MatchString(result.Symbol)
			if match {
				continue
			}

			symbolsPrice[result.Symbol], _ = result.Close.Float64()
			symbolsHigh[result.Symbol], _ = result.High.Float64()
			symbolsLow[result.Symbol], _ = result.Low.Float64()

			symbols = append(symbols, result.Symbol)
		}

		max9daySymbols = []string{}
		for _, result := range symbols {
			goMax9daySync(result)
		}

		applogger.Info("symbols: %+v", max9daySymbols)
	}
}

func goMax9daySync(symbol string) {
	optionalRequest := market.GetCandlestickOptionalRequest{Period: market.DAY1, Size: 9}
	resp, err := MarketClient.GetCandlestick(symbol, optionalRequest)
	if err != nil {
		exit = true
		applogger.Info("%+v", symbol)
		applogger.Error("Error2: %s", err)
	} else {
		maxClose, _ := resp[0].Close.Float64()
		isMax := true
		for key, result := range resp {
			high, _ := result.High.Float64()

			if key != 0 && high > maxClose {
				isMax = false
			}
		}
		if isMax {
			max9daySymbols = append(max9daySymbols, symbol)
		}
	}
}

func balanceSync() {
	client := new(client.AccountClient).Init(config.AccessKey, config.SecretKey, config.Host)
	resp, err := client.GetAccountBalance(config.AccountId)
	if err != nil {
		applogger.Error("Get account balance error: %s", err)
	} else {
		if resp.List != nil {
			for _, result := range resp.List {
				if result.Type != "trade" {
					continue
				}

				currencysBalance[result.Currency], _ = strconv.ParseFloat(result.Balance,64)
			}
		}
	}
}

func infoSync() {
	client := new(client.CommonClient).Init(config.Host)
	resp, err := client.GetSymbols()
	if err != nil {
		exit = true
		applogger.Error("Error3: %s", err)
	} else {
		for _, result := range resp {
			if result.QuoteCurrency != "usdt" {
				continue
			}
			symbolInfo[result.Symbol] = result
		}
	}
}

func goHandle() {
	timeTickerChan := time.Tick(999999999)
    for !exit {
		handle()
        <-timeTickerChan
    }

	wg.Done()
}

func handle() {
	for currency, balance := range currencysBalance {
		symbol := currency + "usdt";
		if price, ok := symbolsPrice[symbol]; ok {
			amount := balance * price
			if IsContain(max9daySymbols, symbol) && amount < float64(DEALNUM) && buyCheck(symbol) {
				if orderId := buyHandle(symbol, price); orderId != "" {
					orderInfo := orderInfoHandle(orderId)
			
					realAmount := orderInfo.FilledAmount
					if realAmount > 0 {
						balanceSync()
					}
				}
			}
			if symbol != "htusdt" && amount >= float64(DEALNUM) * 0.63 && sellCheck(symbol) {
				if orderId := sellHandle(symbol, price, balance); orderId != "" {
					orderInfo := orderInfoHandle(orderId)
			
					realAmount := orderInfo.FilledAmount
					if realAmount > 0 {
						balanceSync()
					}
				}
			}
		}
	}
}

func buyCheck(symbol string) bool {
	if !closeDown3Per(symbol) {
		return true
	}

	return false
}

func sellCheck(symbol string) bool {
	if closeDown9Per(symbol) || closeLow(symbol) {
		return true
	}

	return false
}

func minutes3High(symbol string) bool {
	optionalRequest := market.GetCandlestickOptionalRequest{Period: market.MIN1, Size: 3}
	resp, err := MarketClient.GetCandlestick(symbol, optionalRequest)
	if err != nil {
		exit = true
		applogger.Error("Error4: %s", err)
	} else {
		maxHigh := 0.0
		for _, result := range resp {
			high, _ := result.High.Float64()

			if high > maxHigh {
				maxHigh = high
			}
		}
		if maxHigh >= symbolsHigh[symbol] {
			return true
		}
	}

	return false
}

func closeDown3Per(symbol string) bool {
	if symbolsHigh[symbol] / symbolsPrice[symbol] > 1.02 {
		return true
	}

	return false
}

func closeDown9Per(symbol string) bool {
	if symbolsHigh[symbol] / symbolsPrice[symbol] > 1.08 {
		return true
	}

	return false
}

func closeLow(symbol string) bool {
	if symbolsLow[symbol] >= symbolsPrice[symbol] {
		return true
	}

	return false
}

func buyHandle(symbol string, price float64) (string) {
	if _, ok := symbolInfo[symbol]; !ok {
		infoSync()
	}

	client := new(client.OrderClient).Init(config.AccessKey, config.SecretKey, config.Host)
	
	priceS := strconv.FormatFloat(price,'E',-1,64)

	amountUsdt := float64(DEALNUM)
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
			return resp.Data
		case "error":
			exit = true
			applogger.Error("Place order error: %s", resp.ErrorMessage)
			return ""
		}
	}

	return ""
}

func sellHandle(symbol string, price float64, amount float64) (string) {
	if _, ok := symbolInfo[symbol]; !ok {
		infoSync()
	}

	client := new(client.OrderClient).Init(config.AccessKey, config.SecretKey, config.Host)
	
	priceS := strconv.FormatFloat(price,'E',-1,64)
	amountS := fmt.Sprintf("%."+strconv.Itoa(symbolInfo[symbol].AmountPrecision)+"f", amount)

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
			return resp.Data
		case "error":
			exit = true
			applogger.Error("Place order error: %s", resp.ErrorMessage)
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

func IsContain(items []string, item string) bool {
	for _, eachItem := range items {
		if eachItem == item {
			return true
		}
	}
	return false
}