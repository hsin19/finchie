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
// gives both the valuation and the holdings. Money comes and goes as fiat orders (a SWIFT transfer), coins
// as deposits and withdrawals.
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
	return fold(ctx, b.Institution(), wallets, usdtPrices(markets)), nil
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

type binanceDeposit struct {
	ID         string  `json:"id"`
	Coin       string  `json:"coin"`
	Amount     decimal `json:"amount"`
	Status     int     `json:"status"`
	TxID       string  `json:"txId"`
	InsertTime int64   `json:"insertTime"`
}

type binanceWithdrawal struct {
	ID             string  `json:"id"`
	Coin           string  `json:"coin"`
	Amount         decimal `json:"amount"`
	TransactionFee decimal `json:"transactionFee"`
	Status         int     `json:"status"`
	TxID           string  `json:"txId"`
	// UTC, as "2026-02-16 11:51:00".
	ApplyTime string `json:"applyTime"`
}

// binancePage is as many fiat orders as Binance lists at once; of deposits and withdrawals it lists up to
// 1,000, far more than a month of a person's.
const binancePage = 500

// Movements are Binance's fiat orders, deposits and withdrawals of coins created in the span. A fiat
// deposit is what the bank sent, the fee Binance then took apart; a withdrawal what the bank was sent.
func (b *Binance) Movements(ctx context.Context, from, to time.Time) ([]Movement, error) {
	fiat, err := b.fiatOrders(ctx, from, to)
	if err != nil {
		return nil, err
	}
	coins, err := b.coinMovements(ctx, from, to)
	if err != nil {
		return nil, err
	}
	return append(fiat, coins...), nil
}

func (b *Binance) fiatOrders(ctx context.Context, from, to time.Time) ([]Movement, error) {
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
				"beginTime":       {strconv.FormatInt(from.UnixMilli(), 10)},
				"endTime":         {strconv.FormatInt(to.UnixMilli()-1, 10)},
				"page":            {strconv.Itoa(page)},
				"rows":            {strconv.Itoa(binancePage)},
			}
			if err := b.signed(ctx, "/sapi/v1/fiat/orders", query, &reply); err != nil {
				return nil, err
			}
			for _, o := range reply.Data {
				pending := o.Status == "Processing"
				if !pending && o.Status != "Successful" && o.Status != "Finished" {
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
				fee, err := quantity(string(o.TotalFee))
				if err != nil {
					return nil, fmt.Errorf("binance fiat order %s fee: %w", o.OrderNo, err)
				}
				if !deposits {
					amount.Neg(amount)
				}
				moves = append(moves, Movement{
					ID: o.OrderNo, At: time.UnixMilli(o.CreateTime), Unit: o.FiatCurrency, Amount: amount,
					Fee: fee, FeeUnit: o.FiatCurrency, Memo: o.Method, Pending: pending,
				})
			}
			if len(reply.Data) < binancePage || page*binancePage >= reply.Total {
				break
			}
		}
	}
	return moves, nil
}

// coinMovements are the deposits and withdrawals of coins, on a chain or between Binance's users. Binance
// lists up to 90 days at once.
func (b *Binance) coinMovements(ctx context.Context, from, to time.Time) ([]Movement, error) {
	span := url.Values{
		"startTime": {strconv.FormatInt(from.UnixMilli(), 10)},
		"endTime":   {strconv.FormatInt(to.UnixMilli()-1, 10)},
		"limit":     {"1000"},
	}
	var deposits []binanceDeposit
	if err := b.signed(ctx, "/sapi/v1/capital/deposit/hisrec", cloneQuery(span), &deposits); err != nil {
		return nil, err
	}
	var withdrawals []binanceWithdrawal
	if err := b.signed(ctx, "/sapi/v1/capital/withdraw/history", cloneQuery(span), &withdrawals); err != nil {
		return nil, err
	}
	var moves []Movement
	for _, d := range deposits {
		// 1 success, 6 credited but not yet withdrawable; 0 pending, 8 waiting for the owner; 2 rejected, 7 wrong.
		pending := d.Status == 0 || d.Status == 8
		if !pending && d.Status != 1 && d.Status != 6 {
			continue
		}
		amount, err := quantity(string(d.Amount))
		if err != nil {
			return nil, fmt.Errorf("binance deposit %s: %w", d.ID, err)
		}
		moves = append(moves, Movement{ID: "deposit/" + d.ID, At: time.UnixMilli(d.InsertTime), Unit: d.Coin, Amount: amount, TxID: d.TxID, Pending: pending})
	}
	for _, w := range withdrawals {
		// 6 completed; 0 e-mail sent, 2 awaiting approval, 4 processing; 1 canceled, 3 rejected, 5 failed.
		pending := w.Status == 0 || w.Status == 2 || w.Status == 4
		if !pending && w.Status != 6 {
			continue
		}
		at, err := time.Parse(time.DateTime, w.ApplyTime)
		if err != nil {
			return nil, fmt.Errorf("binance withdrawal %s time: %w", w.ID, err)
		}
		amount, err := quantity(string(w.Amount))
		if err != nil {
			return nil, fmt.Errorf("binance withdrawal %s: %w", w.ID, err)
		}
		fee, err := quantity(string(w.TransactionFee))
		if err != nil {
			return nil, fmt.Errorf("binance withdrawal %s fee: %w", w.ID, err)
		}
		moves = append(moves, Movement{
			ID: "withdrawal/" + w.ID, At: at, Unit: w.Coin, Amount: amount.Neg(amount), Fee: fee, FeeUnit: w.Coin, TxID: w.TxID, Pending: pending,
		})
	}
	return moves, nil
}

func cloneQuery(q url.Values) url.Values {
	c := url.Values{}
	for k, v := range q {
		c[k] = append([]string(nil), v...)
	}
	return c
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
