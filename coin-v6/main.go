package main

import (
	// "fmt"
	"math"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	// "time"
	"github.com/huobirdcenter/huobi_golang/config"
	"github.com/huobirdcenter/huobi_golang/logging/applogger"
	"github.com/huobirdcenter/huobi_golang/pkg/client"
	"github.com/huobirdcenter/huobi_golang/pkg/model/common"
	"github.com/huobirdcenter/huobi_golang/pkg/model/market"
	"github.com/huobirdcenter/huobi_golang/pkg/model/order"
)

const DEALNUM int = 9

var symbolInfo = make(map[string]common.Symbol)
var symbolsPrice = make(map[string]float64)
var currencysBalance = make(map[string]float64)
var MarketClient *client.MarketClient
var AccountClient *client.AccountClient

// var lastDayTs int64
// var exit bool

type OrderInfo struct {
	Amount       float64
	FilledAmount float64
	FilledFees   float64
}

func main() {
	c := make(chan os.Signal)
	signal.Notify(c, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	go func() {
		for s := range c {
			switch s {
			case syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT:
				// exit = true
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

// func setLastDayTs() {
// 	timeStr := time.Now().AddDate(0, 0, -1).Format("20060102")
// 	t, _ := time.ParseInLocation("20060102", timeStr, time.Local)
// 	lastDayTs = t.Unix()
// }

func handle() {
	handler()
}

func handler() {
	resp, err := MarketClient.GetAllSymbolsLast24hCandlesticksAskBid()
	if err != nil {
		// exit = true
		applogger.Error("GetAllSymbolsLast24hCandlesticksAskBidError: %s", err)
	} else {
		maxUpPre := 0.0
		maxUpSymbol := ""
		for _, result := range resp {
			if str4 := result.Symbol[len(result.Symbol)-4:]; str4 != "usdt" {
				continue
			}

			regexp, _ := regexp.Compile(`([\d])[l|s]`)
			match := regexp.MatchString(result.Symbol)
			if match {
				continue
			}

			optionalRequest := market.GetCandlestickOptionalRequest{Period: market.DAY1, Size: 9}
			respI, err := MarketClient.GetCandlestick(result.Symbol, optionalRequest)
			if err != nil {
				// exit = true
				applogger.Error("lastDayAmountError: %s", err)
			} else {
				maxClose := 0.0
				curClose := 0.0
				lastClose := 0.0
				for _, resultI := range respI {
					close, _ := resultI.Close.Float64()
					if (close > maxClose) {
						maxClose = close
					}
					if curClose == 0.0 {
						curClose = close
					} else {
						lastClose = close
					}
				}
				curUpPre := 0.0
				if lastClose != 0.0 {
					curUpPre = curClose / lastClose
				}
				if curClose == maxClose && curUpPre > maxUpPre {
					maxUpPre = curUpPre
					maxUpSymbol = result.Symbol
				}
			}

			close, _ := result.Close.Float64()
			symbolsPrice[result.Symbol] = close
		}
		applogger.Info("%+v %+v", maxUpSymbol, maxUpPre)

		amountUsdt := ""
		maxAmountNum := 0.0
		maxAmountSymbol := ""
		maxAmountBalance := 0.0
		resp, err := AccountClient.GetAccountBalance(config.AccountId)
		if err != nil {
			applogger.Error("Get account balance error: %s", err)
		} else {
			if resp.List != nil {
				for _, result := range resp.List {
					if result.Type != "trade" {
						continue
					}

					if result.Currency == "usdt" {
						amountUsdt = result.Balance
						// amountUsdt, _ = strconv.ParseFloat(result.Balance, 64)
					}
	
					symbol := result.Currency + "usdt"
					if value, ok := symbolsPrice[symbol]; ok {
						balance, _ := strconv.ParseFloat(result.Balance, 64)
						amount := value * balance
						if amount > maxAmountNum {
							maxAmountNum = amount
							maxAmountSymbol = symbol
							maxAmountBalance = balance
						}
					}
				}
			}
		}
		applogger.Info("%+v %+v", maxAmountSymbol, maxAmountNum)

		if maxUpSymbol != maxAmountSymbol {
			if maxAmountNum > 5.0 {
				if orderId := sellHandle(maxAmountSymbol, maxAmountBalance); orderId != "" {
					orderInfo := orderInfoHandle(orderId)
					applogger.Info("sell	%+v", orderInfo)
				}
			} else {
				if orderId := buyHandle(maxUpSymbol, amountUsdt); orderId != "" {
					orderInfo := orderInfoHandle(orderId)
					applogger.Info("buy		%+v", orderInfo)
				}
			}
		}
	}
}

func initClient() {
	MarketClient = new(client.MarketClient).Init(config.Host)
	AccountClient = new(client.AccountClient).Init(config.AccessKey, config.SecretKey, config.Host)
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

				currencysBalance[result.Currency], _ = strconv.ParseFloat(result.Balance, 64)
			}
		}
	}
}

func infoSync() {
	client := new(client.CommonClient).Init(config.Host)
	resp, err := client.GetSymbols()
	if err != nil {
		// exit = true
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

func buyHandle(symbol string, amount string) string {
	if _, ok := symbolInfo[symbol]; !ok {
		infoSync()
	}

	client := new(client.OrderClient).Init(config.AccessKey, config.SecretKey, config.Host)

	index := strings.Index(amount, ".")
	rAmound := amount[0 : index + 9]

	request := order.PlaceOrderRequest{
		AccountId: config.AccountId,
		Type:      "buy-market",
		Source:    "spot-api",
		Symbol:    symbol,
		Amount:    rAmound,
	}

	resp, err := client.PlaceOrder(&request)
	if err != nil {
		// exit = true
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

func sellHandle(symbol string, balance float64) string {
	if _, ok := symbolInfo[symbol]; !ok {
		infoSync()
	}

	client := new(client.OrderClient).Init(config.AccessKey, config.SecretKey, config.Host)

	amountS := FormatFloat(balance, symbolInfo[symbol].AmountPrecision)

	request := order.PlaceOrderRequest{
		AccountId: config.AccountId,
		Type:      "sell-market",
		Source:    "spot-api",
		Symbol:    symbol,
		// Price:     priceS,
		Amount:    amountS,
	}

	resp, err := client.PlaceOrder(&request)
	if err != nil {
		// exit = true
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

func orderInfoHandle(orderId string) OrderInfo {
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

				amount, _ := strconv.ParseFloat(resp.Data.Amount, 64)
				filledAmount, _ := strconv.ParseFloat(resp.Data.FilledAmount, 64)
				filledFees, _ := strconv.ParseFloat(resp.Data.FilledFees, 64)
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
