package exchanges

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Binance values every wallet in USDT, and with needBalanceDetail lists each wallet's coins: one call
// gives both the valuation and the holdings. Money comes and goes as fiat orders (a SWIFT transfer).
type Binance struct {
	apiKey, apiSecret string
	baseURL           string
	client            *http.Client
}

func NewBinance(apiKey, apiSecret string, client *http.Client) *Binance {
	return &Binance{apiKey: apiKey, apiSecret: apiSecret, baseURL: "https://api.binance.com", client: client}
}

func (*Binance) Institution() string { return "binance" }

type binanceWallet struct {
	WalletName    string  `json:"walletName"`
	Balance       decimal `json:"balance"`
	AssetBalances []struct {
		Asset       string  `json:"asset"`
		Free        decimal `json:"free"`
		Locked      decimal `json:"locked"`
		Freeze      decimal `json:"freeze"`
		Withdrawing decimal `json:"withdrawing"`
	} `json:"assetBalances"`
}

func (b *Binance) Fetch(ctx context.Context) (Snapshot, error) {
	var balances []binanceWallet
	query := url.Values{"quoteAsset": {"USDT"}, "needBalanceDetail": {"true"}}
	if err := b.signed(ctx, "/sapi/v1/asset/wallet/balance", query, &balances); err != nil {
		return Snapshot{}, err
	}
	markets, err := b.markets(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	wallets := make([]wallet, 0, len(balances))
	for _, w := range balances {
		value, err := quantity(string(w.Balance))
		if err != nil {
			return Snapshot{}, fmt.Errorf("binance %s: %w", w.WalletName, err)
		}
		listed := wallet{name: w.WalletName, value: value}
		if len(w.AssetBalances) > 0 {
			listed.coins = map[string]*big.Rat{}
		}
		for _, a := range w.AssetBalances {
			amount, err := sum(string(a.Free), string(a.Locked), string(a.Freeze), string(a.Withdrawing))
			if err != nil {
				return Snapshot{}, fmt.Errorf("binance %s %s: %w", w.WalletName, a.Asset, err)
			}
			if held, ok := listed.coins[a.Asset]; ok {
				held.Add(held, amount)
			} else {
				listed.coins[a.Asset] = amount
			}
		}
		wallets = append(wallets, listed)
	}
	snapshot := fold(ctx, b.Institution(), wallets, usdtPrices(markets))
	snapshot.Movements, err = b.movements(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	return snapshot, nil
}

type binanceFiatOrder struct {
	OrderNo      string `json:"orderNo"`
	FiatCurrency string `json:"fiatCurrency"`
	// What was asked for, and what is left of it once the fee is taken.
	IndicatedAmount decimal `json:"indicatedAmount"`
	Amount          decimal `json:"amount"`
	TotalFee        decimal `json:"totalFee"`
	Method          string  `json:"method"`
	Status          string  `json:"status"`
	CreateTime      int64   `json:"createTime"`
}

// binanceOpened is when Binance opened: fiat orders are asked for from then on, every one each time.
var binanceOpened = time.Date(2017, 7, 1, 0, 0, 0, 0, time.UTC)

// binancePage is as many fiat orders as Binance lists at once.
const binancePage = 500

// movements are Binance's fiat deposits and withdrawals that went through: what the bank sent, the
// fee Binance then took in the memo; what the bank was sent, after the fee.
func (b *Binance) movements(ctx context.Context) ([]Movement, error) {
	var moves []Movement
	for _, deposits := range []bool{true, false} {
		kind := map[bool]string{true: "0", false: "1"}[deposits]
		for page := 1; ; page++ {
			var reply struct {
				Data  []binanceFiatOrder `json:"data"`
				Total int                `json:"total"`
			}
			query := url.Values{
				"transactionType": {kind},
				"beginTime":       {strconv.FormatInt(binanceOpened.UnixMilli(), 10)},
				"endTime":         {strconv.FormatInt(time.Now().UnixMilli(), 10)},
				"page":            {strconv.Itoa(page)},
				"rows":            {strconv.Itoa(binancePage)},
			}
			if err := b.signed(ctx, "/sapi/v1/fiat/orders", query, &reply); err != nil {
				return nil, err
			}
			for _, o := range reply.Data {
				if o.Status != "Successful" && o.Status != "Finished" {
					continue
				}
				printed := o.Amount
				if deposits {
					printed = o.IndicatedAmount
				}
				amount, err := quantity(string(printed))
				if err != nil {
					return nil, fmt.Errorf("binance fiat order %s: %w", o.OrderNo, err)
				}
				if !deposits {
					amount.Neg(amount)
				}
				memo := o.Method
				if fee := feeMemo(string(o.TotalFee), o.FiatCurrency); fee != "" {
					memo += " · " + fee
				}
				moves = append(moves, Movement{ID: o.OrderNo, At: time.UnixMilli(o.CreateTime), Unit: o.FiatCurrency, Amount: amount, Memo: memo})
			}
			if len(reply.Data) < binancePage || page*binancePage >= reply.Total {
				break
			}
		}
	}
	return moves, nil
}

// markets are every spot market's last price, from the public ticker.
func (b *Binance) markets(ctx context.Context) (map[string]*big.Rat, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, b.baseURL+"/api/v3/ticker/price", nil)
	if err != nil {
		return nil, fmt.Errorf("binance ticker: %w", err)
	}
	var tickers []struct {
		Symbol string  `json:"symbol"`
		Price  decimal `json:"price"`
	}
	if err := get(b.client, request, &tickers); err != nil {
		return nil, fmt.Errorf("binance: %w", err)
	}
	markets := make(map[string]*big.Rat, len(tickers))
	for _, t := range tickers {
		if price, err := quantity(string(t.Price)); err == nil {
			markets[t.Symbol] = price
		}
	}
	return markets, nil
}

// signed sends a GET to a private endpoint: Binance signs the query string, hex, as its last parameter.
func (b *Binance) signed(ctx context.Context, path string, query url.Values, into any) error {
	query.Set("recvWindow", "10000")
	query.Set("timestamp", strconv.FormatInt(time.Now().UnixMilli(), 10))
	encoded := query.Encode()
	mac := hmac.New(sha256.New, []byte(b.apiSecret))
	mac.Write([]byte(encoded))
	target := b.baseURL + path + "?" + encoded + "&signature=" + hex.EncodeToString(mac.Sum(nil))
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return fmt.Errorf("binance %s: %w", path, err)
	}
	request.Header.Set("X-MBX-APIKEY", b.apiKey)
	if err := get(b.client, request, into); err != nil {
		return fmt.Errorf("binance: %w", err)
	}
	return nil
}
