package main

import (
	"fmt"
	"os"
	"os/signal"
	"regexp"
	"sort"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/huobirdcenter/huobi_golang/config"
	"github.com/huobirdcenter/huobi_golang/logging/applogger"
	"github.com/huobirdcenter/huobi_golang/pkg/client"
	"github.com/huobirdcenter/huobi_golang/pkg/model/common"
	"github.com/huobirdcenter/huobi_golang/pkg/model/market"
	"github.com/huobirdcenter/huobi_golang/pkg/model/order"
)

const CLOSENUM int = 36
const AMOUNTUSDT int = 9
  
var wg sync.WaitGroup
var wgKline sync.WaitGroup

var CommonClient *client.CommonClient
var MarketClient *client.MarketClient
var AccountClient *client.AccountClient
var OrderClient *client.OrderClient

var symbols []string
var symbolBuy = make(map[string]int)
var symbolInfo = make(map[string]common.Symbol)
var symbolAmount = make(map[string]float64)
var symbolEma = make(map[string][]Ema)

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
	Id 		int64
	Close	float64
	Ema3 	float64
	Ema6 	float64
	Ema9 	float64
}

type Pair struct {
    Key   string
    Value int
}

type PairList []Pair

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

	CommonClient = new(client.CommonClient).Init(config.Host)
	MarketClient = new(client.MarketClient).Init(config.Host)
	AccountClient = new(client.AccountClient).Init(config.AccessKey, config.SecretKey, config.Host)
	OrderClient = new(client.OrderClient).Init(config.AccessKey, config.SecretKey, config.Host)

	marketSync()

	wg.Add(3)
	go goSymbolInfo()
	go goMarket()
	go goKline()
	wg.Wait()
}

func goSymbolInfo() {
	symbolInfoSync()
	wg.Done()
}

func goMarket() {
	timeTickerChan := time.Tick(time.Second * 6)
    for !exit {
		marketSync()
        <-timeTickerChan
    }
	wg.Done()
}

func goKline() {
	timeTickerChan := time.Tick(time.Second * 3)
    for !exit {
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

func symbolInfoSync() {
	resp, err := CommonClient.GetSymbols()
	if err != nil {
		exit = true
		applogger.Error("GetSymbolsError: %s", err)
	} else {
		for _, result := range resp {
			if result.QuoteCurrency != "usdt" {
				continue
			}
			symbolInfo[result.Symbol] = result
		}
	}
}

func marketSync() {
	resp, err := MarketClient.GetAllSymbolsLast24hCandlesticksAskBid()
	if err != nil {
		exit = true
		applogger.Error("GetAllSymbolsLast24hCandlesticksAskBidError: %s", err)
	} else {
		symbolUp := make(map[string]int)
		var tSymbols []string
		for _, result := range resp {
			if str4 := result.Symbol[len(result.Symbol)-4:]; str4 != "usdt" {
				continue
			}

			regexp, _ := regexp.Compile(`([\d])[l|s]`)
			match := regexp.MatchString(result.Symbol)
			if match {
				continue
			}

			open, _ := result.Open.Float64()
			close, _ := result.Close.Float64()
			upPre := int(close / open * 100)

			if upPre > 100 {
				symbolUp[result.Symbol] = upPre
			}
		}
		sortedSymbol := sortMapByValue(symbolUp)

		for _, symbol := range sortedSymbol[len(sortedSymbol) - 1:] {
			tSymbols = append(tSymbols, symbol.Key)
		}
		symbols = tSymbols
	}
}

func kline(symbol string) {
	num := CLOSENUM
	init := true
	var emaList []Ema
	if value, ok := symbolEma[symbol]; ok {
		num = 1
		init = false
		emaList = value
	}

	optionalRequest := market.GetCandlestickOptionalRequest{Period: market.MIN1, Size: num}
	resp, err := MarketClient.GetCandlestick(symbol, optionalRequest)
	if err != nil {
		exit = true
		applogger.Error("GetCandlestickError: %s", err)
	} else {
		// abc := Ema{1, 1.2, 1.3, 1.3, 1.3}
		// symbolEma["abc"] = append(symbolEma["abc"], abc)

		// abc1 := Ema{2, 1.2, 1.3, 1.3, 1.3}
		// symbolEma["abc"] = append(symbolEma["abc"], abc1)

		// abc2 := Ema{3, 1.2, 1.3, 1.3, 1.3}
		// symbolEma["abc"] = append(symbolEma["abc"], abc2)

		// abc3 := Ema{4, 1.2, 1.3, 1.3, 1.3}
		// symbolEma["abc"] = append(symbolEma["abc"][1:], abc3)

		// applogger.Info("%+v", symbolEma)
		// applogger.Info("%+v", symbolEma["abc"][2])
	
		// os.Exit(0)

		// var buy = symbolBuy[symbol]

		for _, result := range resp {
			close, _ := result.Close.Float64()

			var preEma Ema
			var idNew bool = true
			var preEmaKey int
			if count := len(emaList); count > 0 {
				preEmaKey = count - 1
				preEma = emaList[preEmaKey]
				if preEma.Id == result.Id {
					idNew = false
					preEmaKey = count - 2
					preEma = emaList[preEmaKey]
				}
			} else {
				preEma = Ema{result.Id, close, close, close, close}
			}

			curEma := Ema{
				result.Id,
				close,
				2.0 / (3 + 1) * close + (3.0 - 1) / (3 + 1) * preEma.Ema3,
				2.0 / (6 + 1) * close + (6.0 - 1) / (6 + 1) * preEma.Ema6,
				2.0 / (9 + 1) * close + (9.0 - 1) / (9 + 1) * preEma.Ema9}


			if init {
				emaList = append(emaList, curEma)
			} else {
				if idNew {
					emaList = append(emaList[1:], curEma)
				} else {
					emaList[preEmaKey + 1] = curEma
				}
			}

			applogger.Info("%+v", symbol)

			applogger.Info("%+v, %+v, %+v", symbol, emaList, idNew)

			// close, _ := result.Close.Float64()

			// var emaInfo Ema
			// if value, ok := symbolEma[symbol]; ok {
			// 	emaInfo = value
			// } else {
			// 	emaInfo = Ema{0, close, close, close, close}
			// }

			// currentEma := Ema{
			// 	close,
			// 	2.0 / (36 + 1) * close + (36.0 - 1) / (36 + 1) * emaInfo.Ema3,
			// 	2.0 / (63 + 1) * close + (63.0 - 1) / (63 + 1) * emaInfo.Ema6,
			// 	2.0 / (96 + 1) * close + (96.0 - 1) / (96 + 1) * emaInfo.Ema9}

			// symbolEma[symbol] = currentEma

			// applogger.Info("%+v", symbolEma)

			// if !init && symbolEmaTimes[symbol] >= CLOSENUM {
			// 	if currentEma.Ema39 < currentEma.Ema66 && currentEma.Ema66 < currentEma.Ema93 {
			// 		if buy == 1 {
			// 			orderId := sellHandle(symbol, close)
			// 			if orderId != "" {
			// 				orderInfo := orderInfoHandle(orderId)
					
			// 				realAmount := orderInfo.FilledAmount
			// 				if realAmount > 0 {
			// 					amount := 0.0
			// 					if value, ok := symbolAmount[symbol]; ok {
			// 						amount = value - 0.0001
			// 					}
			// 					symbolAmount[symbol], _ = strconv.ParseFloat(fmt.Sprintf("%."+strconv.Itoa(symbolInfo[symbol].AmountPrecision)+"f", amount - realAmount),64)
		
			// 					minAmount, _ := symbolInfo[symbol].LimitOrderMinOrderAmt.Float64()
			// 					if symbolAmount[symbol] <= minAmount {
			// 						buy = 0
			// 					}
			// 				}
			// 			}
			// 		}
			// 	} else if currentEma.Ema333 > currentEma.Ema666 && currentEma.Ema666 > currentEma.Ema999 {
			// 		if buy == 0 {
			// 			orderId := buyHandle(symbol, close)
			// 			if orderId != "" {
			// 				orderInfo := orderInfoHandle(orderId)
					
			// 				realAmount := orderInfo.FilledAmount
			// 				if realAmount > 0 {
			// 					amount := 0.0
			// 					if value, ok := symbolAmount[symbol]; ok {
			// 						amount = value
			// 					}
			// 					symbolAmount[symbol], _ = strconv.ParseFloat(fmt.Sprintf("%."+strconv.Itoa(symbolInfo[symbol].AmountPrecision)+"f", amount + realAmount),64)
			// 					minAmount, _ := symbolInfo[symbol].LimitOrderMinOrderAmt.Float64()
			// 					if symbolAmount[symbol] >= minAmount {
			// 						buy = 1
			// 					}
			// 				}
			// 			}
			// 		}
			// 	} 
			// 	symbolBuy[symbol] = buy
			// }

			// if !init {
			// 	symbolEmaTimes[symbol] ++

			// 	applogger.Info("%s: %f %d %d %+v", symbol, close, symbolEmaTimes[symbol], buy, currentEma)
			// }
		}
	}
	symbolEma[symbol] = emaList
	wgKline.Done()
}

func buyHandle(symbol string, price float64) (string) {
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

	resp, err := OrderClient.PlaceOrder(&request)
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

	resp, err := OrderClient.PlaceOrder(&request)
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
	resp, err := OrderClient.GetOrderById(orderId)
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

func sortMapByValue(m map[string]int) PairList {
    p := make(PairList, len(m))
    i := 0
    for k, v := range m {
        p[i] = Pair{k, v}
		i++
    }
    sort.Sort(p)
    return p
}

func (p PairList) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }

func (p PairList) Len() int           { return len(p) }

func (p PairList) Less(i, j int) bool { return p[i].Value < p[j].Value }