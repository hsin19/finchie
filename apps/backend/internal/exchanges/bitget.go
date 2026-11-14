package exchanges

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Bitget values each of its accounts (spot, funding, earn, futures, bots, …) in USDT; spot, funding, and
// earn list their coins on endpoints of their own.
type Bitget struct {
	apiKey, apiSecret, passphrase string
	baseURL                       string
	client                        *http.Client
}

func NewBitget(apiKey, apiSecret, passphrase string, client *http.Client) *Bitget {
	return &Bitget{apiKey: apiKey, apiSecret: apiSecret, passphrase: passphrase, baseURL: "https://api.bitget.com", client: client}
}

func (*Bitget) Institution() string { return "bitget" }

// bitgetReply is every reply's envelope; a refusal can come with 200 and a code other than 00000.
type bitgetReply struct {
	Code string          `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

type bitgetCoin struct {
	Coin      string  `json:"coin"`
	Available decimal `json:"available"`
	Frozen    decimal `json:"frozen"`
	Locked    decimal `json:"locked"`
	Amount    decimal `json:"amount"`
}

func (g *Bitget) Fetch(ctx context.Context) (Snapshot, error) {
	var accounts []struct {
		AccountType string  `json:"accountType"`
		USDTBalance decimal `json:"usdtBalance"`
	}
	if err := g.signed(ctx, "/api/v2/account/all-account-balance", nil, &accounts); err != nil {
		return Snapshot{}, err
	}
	// The accounts that list their coins, by the accountType that values them. A coin's total is what is
	// available and what orders or the exchange hold of it.
	listings := map[string]string{
		"spot":    "/api/v2/spot/account/assets",
		"funding": "/api/v2/account/funding-assets",
		"earn":    "/api/v2/earn/account/assets",
	}
	coinsOf := map[string]map[string]*big.Rat{}
	for accountType, path := range listings {
		var coins []bitgetCoin
		if err := g.signed(ctx, path, nil, &coins); err != nil {
			return Snapshot{}, err
		}
		listed := map[string]*big.Rat{}
		for _, c := range coins {
			amount, err := sum(string(c.Available), string(c.Frozen), string(c.Locked), string(c.Amount))
			if err != nil {
				return Snapshot{}, fmt.Errorf("bitget %s %s: %w", accountType, c.Coin, err)
			}
			if held, ok := listed[c.Coin]; ok {
				held.Add(held, amount)
			} else {
				listed[c.Coin] = amount
			}
		}
		coinsOf[accountType] = listed
	}
	markets, err := g.markets(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	wallets := make([]wallet, 0, len(accounts))
	for _, a := range accounts {
		value, err := quantity(string(a.USDTBalance))
		if err != nil {
			return Snapshot{}, fmt.Errorf("bitget %s: %w", a.AccountType, err)
		}
		wallets = append(wallets, wallet{name: a.AccountType, coins: coinsOf[a.AccountType], value: value})
	}
	return fold(ctx, g.Institution(), wallets, usdtPrices(markets)), nil
}

// markets are every spot market's last price, from the public tickers.
func (g *Bitget) markets(ctx context.Context) (map[string]*big.Rat, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, g.baseURL+"/api/v2/spot/market/tickers", nil)
	if err != nil {
		return nil, fmt.Errorf("bitget tickers: %w", err)
	}
	var tickers []struct {
		Symbol string  `json:"symbol"`
		LastPr decimal `json:"lastPr"`
	}
	if err := g.unwrap(request, &tickers, false); err != nil {
		return nil, err
	}
	markets := make(map[string]*big.Rat, len(tickers))
	for _, t := range tickers {
		if price, err := quantity(string(t.LastPr)); err == nil {
			markets[t.Symbol] = price
		}
	}
	return markets, nil
}

type bitgetMovement struct {
	OrderID string `json:"orderId"`
	// The chain's transaction, or Bitget's own for a transfer between its users.
	TradeID string  `json:"tradeId"`
	Coin    string  `json:"coin"`
	Size    decimal `json:"size"`
	// A withdrawal's, printed negative.
	Fee    decimal `json:"fee"`
	Status string  `json:"status"`
	CTime  decimal `json:"cTime"`
}

// bitgetPage is as many deposits or withdrawals as Bitget lists at once.
const bitgetPage = 100

// Movements are Bitget's deposits and withdrawals of coins created in the span; Bitget lists up to 90 days
// at once, a page at a time.
func (g *Bitget) Movements(ctx context.Context, from, to time.Time) ([]Movement, error) {
	var moves []Movement
	for _, deposits := range []bool{true, false} {
		path := map[bool]string{true: "/api/v2/spot/wallet/deposit-records", false: "/api/v2/spot/wallet/withdrawal-records"}[deposits]
		var before string
		for {
			query := url.Values{
				"startTime": {strconv.FormatInt(from.UnixMilli(), 10)},
				"endTime":   {strconv.FormatInt(to.UnixMilli()-1, 10)},
				"limit":     {strconv.Itoa(bitgetPage)},
			}
			if before != "" {
				query.Set("idLessThan", before)
			}
			var page []bitgetMovement
			if err := g.signedList(ctx, path, query, &page); err != nil {
				return nil, err
			}
			for _, r := range page {
				pending := r.Status == "pending"
				if !pending && r.Status != "success" {
					continue
				}
				move, err := r.movement(deposits, pending)
				if err != nil {
					return nil, err
				}
				moves = append(moves, move)
			}
			if len(page) < bitgetPage {
				break
			}
			before = page[len(page)-1].OrderID
		}
	}
	return moves, nil
}

func (r bitgetMovement) movement(deposit, pending bool) (Movement, error) {
	created, ok := new(big.Int).SetString(string(r.CTime), 10)
	if !ok {
		return Movement{}, fmt.Errorf("bitget %s: time %q", r.OrderID, r.CTime)
	}
	amount, err := quantity(string(r.Size))
	if err != nil {
		return Movement{}, fmt.Errorf("bitget %s: %w", r.OrderID, err)
	}
	move := Movement{At: time.UnixMilli(created.Int64()), Unit: r.Coin, Amount: amount, TxID: r.TradeID, Pending: pending}
	if deposit {
		move.ID = "deposit/" + r.OrderID
		return move, nil
	}
	fee, err := quantity(string(r.Fee))
	if err != nil {
		return Movement{}, fmt.Errorf("bitget %s fee: %w", r.OrderID, err)
	}
	move.ID, move.Amount, move.Fee, move.FeeUnit = "withdrawal/"+r.OrderID, amount.Neg(amount), fee.Abs(fee), r.Coin
	return move, nil
}

// signed sends a GET to a private endpoint that answers with what a snapshot needs: no data is an error.
func (g *Bitget) signed(ctx context.Context, path string, query url.Values, into any) error {
	return g.send(ctx, path, query, into, false)
}

// signedList sends a GET to a private endpoint that lists records, of which there may be none.
func (g *Bitget) signedList(ctx context.Context, path string, query url.Values, into any) error {
	return g.send(ctx, path, query, into, true)
}

// send signs a GET: Bitget signs timestamp, method, path, and query, base64.
func (g *Bitget) send(ctx context.Context, path string, query url.Values, into any, list bool) error {
	requestPath := path
	if len(query) > 0 {
		requestPath += "?" + query.Encode()
	}
	timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
	mac := hmac.New(sha256.New, []byte(g.apiSecret))
	mac.Write([]byte(timestamp + http.MethodGet + requestPath))
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, g.baseURL+requestPath, nil)
	if err != nil {
		return fmt.Errorf("bitget %s: %w", path, err)
	}
	request.Header.Set("ACCESS-KEY", g.apiKey)
	request.Header.Set("ACCESS-SIGN", base64.StdEncoding.EncodeToString(mac.Sum(nil)))
	request.Header.Set("ACCESS-TIMESTAMP", timestamp)
	request.Header.Set("ACCESS-PASSPHRASE", g.passphrase)
	request.Header.Set("locale", "en-US")
	return g.unwrap(request, into, list)
}

func (g *Bitget) unwrap(request *http.Request, into any, list bool) error {
	var reply bitgetReply
	if err := get(g.client, request, &reply); err != nil {
		return fmt.Errorf("bitget: %w", err)
	}
	if reply.Code != "00000" {
		return fmt.Errorf("bitget: GET %s: %s (code %s)", request.URL.Path, reply.Msg, reply.Code)
	}
	// A snapshot is the whole account or nothing (Exchange): a reply that says nothing of a wallet would
	// read as its coins sold. A list may be empty.
	if len(reply.Data) == 0 || string(reply.Data) == "null" {
		if list {
			return nil
		}
		return fmt.Errorf("bitget: GET %s: no data", request.URL.Path)
	}
	if err := json.Unmarshal(reply.Data, into); err != nil {
		return fmt.Errorf("bitget: GET %s: decode: %w", request.URL.Path, err)
	}
	return nil
}
