package main

import (
	"os"
	"os/signal"
	"syscall"
	"sync"
	"math"
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
var currencysBalance = make(map[string]float64)
var MarketClient *client.MarketClient
var btcUp = 0.00

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
	handle()
}

func handle() {
	timeTickerChan := time.Tick(999999999)
    for !exit {
		handler()
        <-timeTickerChan
    }
}

func handler() {
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

			regexp, _ := regexp.Compile(`([\d])[l|s]`)
			match := regexp.MatchString(result.Symbol)
			if match {
				continue
			}

			close, _ := result.Close.Float64()
			symbolsPrice[result.Symbol] = close
			symbolsHigh[result.Symbol], _ = result.High.Float64()
			symbolsLow[result.Symbol], _ = result.Low.Float64()

			if result.Symbol == "btcusdt" {
				open, _ := result.Open.Float64()
				btcUp = (close - open) / open
			}

			symbols = append(symbols, result.Symbol)

			if sellCheck(result.Symbol) {
				if orderId := sellHandle(result.Symbol, close); orderId != "" {
					orderInfo := orderInfoHandle(orderId)
			
					realAmount := orderInfo.FilledAmount
					if realAmount > 0 {
						balanceSync()
					}
				}
			}

			if result.Vol.IntPart() < minVol() {
				continue
			}

			if buyCheck(result.Symbol) {
				if orderId := buyHandle(result.Symbol, close); orderId != "" {
					orderInfo := orderInfoHandle(orderId)
			
					realAmount := orderInfo.FilledAmount
					if realAmount > 0 {
						balanceSync()
					}
				}
			}
		}

		if !balanceCheck() {
			exit = true
		}
	}
}

func minVol() int64 {
	num := 999999 * 3
	if btcUp > 0.03 {
		num *= 3
	}

	return int64(num)
}

func buyCheck(symbol string) bool {
	if balanceCheck() && !dealAmount(symbol, true) && colseToHigh(symbol) && !close24HoursLow(symbol) && maxNDay(symbol) {
		return true
	}

	return false
}

func sellCheck(symbol string) bool {
	if dealAmount(symbol, false) && (downSell(symbol) || !colseToHigh(symbol) && close24HoursLow(symbol)) {
		return true
	}

	return false
}

func downSell(symbol string) bool {
	currency := symbol[0 : len(symbol) - 4]
	balance := currencysBalance[currency]

	per := (symbolsPrice[symbol] * balance - float64(DEALNUM)) / float64(DEALNUM)
	return per <= -0.09
}

func balanceCheck() bool {
	return currencysBalance["usdt"] >= float64(DEALNUM)
}

func dealAmount(symbol string, buy bool) bool {
	currency := symbol[0 : len(symbol) - 4]

	balance := currencysBalance[currency]
	amount := balance * symbolsPrice[symbol]

	minAmount, _ := symbolInfo[symbol].MinOrderValue.Float64()
	return amount >= minAmount
}

func colseToHigh(symbol string) bool {
	return symbolsPrice[symbol] / symbolsHigh[symbol] > 0.99
}

func close24HoursLow(symbol string) bool {
	optionalRequest := market.GetCandlestickOptionalRequest{Period: market.HOUR4, Size: 6}
	resp, err := MarketClient.GetCandlestick(symbol, optionalRequest)
	if err != nil {
		os.Exit(1)
	} else {
		close, _ := resp[0].Low.Float64()
		low := close
		for _, result := range resp {
			h4Low, _ := result.Low.Float64()

			if h4Low < low {
				low = h4Low
			}
		}

		return low / close >= 0.99
	}

	return false
}

func maxNDay(symbol string) bool {
	if btcUp > -0.03 {
		return max3day(symbol)
	} else if btcUp > -0.06 {
		return max6day(symbol)
	} else if btcUp > -0.09 {
		return max9day(symbol)
	} else if symbol == "btcusdt" {
		return btcusdtDown9Up3low()
	}

	return false
}

func max3day(symbol string) bool {
	optionalRequest := market.GetCandlestickOptionalRequest{Period: market.DAY1, Size: 3}
	resp, err := MarketClient.GetCandlestick(symbol, optionalRequest)
	if err != nil {
		exit = true
		applogger.Error("max3dayError: %s", err)
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
			return true
		}
	}

	return false
}

func max6day(symbol string) bool {
	optionalRequest := market.GetCandlestickOptionalRequest{Period: market.DAY1, Size: 6}
	resp, err := MarketClient.GetCandlestick(symbol, optionalRequest)
	if err != nil {
		exit = true
		applogger.Error("max6dayError: %s", err)
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
			return true
		}
	}

	return false
}

func max9day(symbol string) bool {
	optionalRequest := market.GetCandlestickOptionalRequest{Period: market.DAY1, Size: 9}
	resp, err := MarketClient.GetCandlestick(symbol, optionalRequest)
	if err != nil {
		exit = true
		applogger.Error("max9dayError: %s", err)
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
			return true
		}
	}

	return false
}

func btcusdtDown9Up3low() bool {
	if btcUp < -0.09 {
		per := (symbolsPrice["btcusdt"] - symbolsLow["btcusdt"]) / symbolsLow["btcusdt"]
		return per > 0.03
	}

	return false
}

func initClient() {
	MarketClient = new(client.MarketClient).Init(config.Host)
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
			applogger.Info("Buy: %s", symbol)
			return resp.Data
		case "error":
			applogger.Error("Place order error: %s", resp.ErrorMessage)
			return ""
		}
	}

	return ""
}

func sellHandle(symbol string, price float64) (string) {
	if _, ok := symbolInfo[symbol]; !ok {
		infoSync()
	}

	client := new(client.OrderClient).Init(config.AccessKey, config.SecretKey, config.Host)
	
	priceS := strconv.FormatFloat(price,'E',-1,64)

	currency := symbol[0 : len(symbol) - 4]
	balance := currencysBalance[currency]

	amountS := FormatFloat(balance, symbolInfo[symbol].AmountPrecision);

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
			applogger.Info("Sell: %s", symbol)
			return resp.Data
		case "error":
			balanceSync()
			applogger.Error("Place order error: %s   %+v", resp.ErrorMessage, amountS)
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
			balanceSync()
			applogger.Error("Get order by id error: %s", resp.ErrorMessage)
		}
	}

	return OrderInfo{}
}

func FormatFloat(num float64, decimal int) string {
	d := float64(1)
	if decimal > 0 {
	 	d = math.Pow10(decimal)
	}
	return strconv.FormatFloat(math.Trunc(num*d)/d, 'f', -1, 64)
}