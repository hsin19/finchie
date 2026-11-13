package exchanges

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// MAX (MaiCoin) holds coins in its spot wallet. It prints no total of its own, so its valuation is the
// wallet at MAX's own markets in NT$, as InTheGreenYet worked it out. Money comes and goes in NT$, through
// a virtual account at a bank of MAX's.
type MAX struct {
	accessKey, secretKey string
	baseURL              string
	client               *http.Client
}

func NewMAX(accessKey, secretKey string, client *http.Client) *MAX {
	return &MAX{accessKey: accessKey, secretKey: secretKey, baseURL: "https://max-api.maicoin.com", client: client}
}

func (*MAX) Institution() string { return "max" }

type maxAccount struct {
	Currency string  `json:"currency"`
	Balance  decimal `json:"balance"`
	Locked   decimal `json:"locked"`
	Staked   decimal `json:"staked"`
}

func (m *MAX) Fetch(ctx context.Context) (Snapshot, error) {
	var accounts []maxAccount
	if err := m.signed(ctx, "/api/v3/wallet/spot/accounts", nil, &accounts); err != nil {
		return Snapshot{}, err
	}
	prices, err := m.prices(ctx)
	if err != nil {
		return Snapshot{}, err
	}

	var snapshot Snapshot
	worth, priced := new(big.Rat), true
	for _, a := range accounts {
		amount, err := sum(string(a.Balance), string(a.Locked), string(a.Staked))
		if err != nil {
			return Snapshot{}, fmt.Errorf("max %s: %w", a.Currency, err)
		}
		if amount.Sign() == 0 {
			continue
		}
		snapshot.add(a.Currency, amount)
		price, ok := maxPrice(prices, strings.ToLower(a.Currency))
		if !ok {
			// Leaving it out would understate the whole; better no valuation than a wrong one.
			priced = false
			continue
		}
		worth.Add(worth, new(big.Rat).Mul(amount, price))
		snapshot.price(a.Currency, "TWD", price)
	}
	if priced {
		snapshot.Valuation = &Value{Unit: "TWD", Amount: worth}
	}
	snapshot.Movements, err = m.movements(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	return snapshot, nil
}

type maxDeposit struct {
	UUID   string  `json:"uuid"`
	Amount decimal `json:"amount"`
	// The virtual account the money was sent to.
	ToAddress string `json:"to_address"`
	CreatedAt int64  `json:"created_at"`
	State     string `json:"state"`
}

type maxWithdrawal struct {
	UUID        string  `json:"uuid"`
	Amount      decimal `json:"amount"`
	Fee         decimal `json:"fee"`
	FeeCurrency string  `json:"fee_currency"`
	CreatedAt   int64   `json:"created_at"`
	State       string  `json:"state"`
}

// maxHistory is how many of each MAX lists, the newest first: far more than come in between two fetches.
const maxHistory = 1000

// movements are MAX's NT$ deposits and withdrawals that are done.
func (m *MAX) movements(ctx context.Context) ([]Movement, error) {
	params := map[string]any{"currency": "twd", "order": "desc", "limit": maxHistory}
	var deposits []maxDeposit
	if err := m.signed(ctx, "/api/v3/deposits", params, &deposits); err != nil {
		return nil, err
	}
	var withdrawals []maxWithdrawal
	if err := m.signed(ctx, "/api/v3/withdrawals", params, &withdrawals); err != nil {
		return nil, err
	}
	var moves []Movement
	for _, d := range deposits {
		if d.State != "done" {
			continue
		}
		amount, err := quantity(string(d.Amount))
		if err != nil {
			return nil, fmt.Errorf("max deposit %s: %w", d.UUID, err)
		}
		moves = append(moves, Movement{ID: "deposit/" + d.UUID, At: time.UnixMilli(d.CreatedAt), Unit: "TWD", Amount: amount, Memo: d.ToAddress})
	}
	for _, w := range withdrawals {
		if w.State != "done" {
			continue
		}
		amount, err := quantity(string(w.Amount))
		if err != nil {
			return nil, fmt.Errorf("max withdrawal %s: %w", w.UUID, err)
		}
		moves = append(moves, Movement{
			ID: "withdrawal/" + w.UUID, At: time.UnixMilli(w.CreatedAt), Unit: "TWD", Amount: amount.Neg(amount),
			Memo: feeMemo(string(w.Fee), w.FeeCurrency),
		})
	}
	return moves, nil
}

// maxPrice is a coin's price in NT$: its TWD market, else its USDT market at USDT's.
func maxPrice(prices map[string]*big.Rat, coin string) (*big.Rat, bool) {
	if coin == "twd" {
		return big.NewRat(1, 1), true
	}
	if p, ok := prices[coin+"twd"]; ok {
		return p, true
	}
	inUSDT, ok := prices[coin+"usdt"]
	usdt, okUSDT := prices["usdttwd"]
	if !ok || !okUSDT {
		return nil, false
	}
	return new(big.Rat).Mul(inUSDT, usdt), true
}

// prices are every market's last price, from the public v2 tickers: v3 wants the markets named.
func (m *MAX) prices(ctx context.Context) (map[string]*big.Rat, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, m.baseURL+"/api/v2/tickers", nil)
	if err != nil {
		return nil, fmt.Errorf("max tickers: %w", err)
	}
	var tickers map[string]struct {
		Last decimal `json:"last"`
	}
	if err := get(m.client, request, &tickers); err != nil {
		return nil, fmt.Errorf("max: %w", err)
	}
	prices := make(map[string]*big.Rat, len(tickers))
	for market, t := range tickers {
		last, err := quantity(string(t.Last))
		if err != nil || last.Sign() <= 0 {
			continue // a market that has not traded prices nothing
		}
		prices[market] = last
	}
	return prices, nil
}

// signed sends a GET to a private endpoint. MAX signs the base64 of a JSON payload carrying the path, a
// nonce, and the query's parameters, and wants the signature in hex.
func (m *MAX) signed(ctx context.Context, path string, params map[string]any, into any) error {
	nonce := time.Now().UnixMilli()
	body := map[string]any{"nonce": nonce, "path": path}
	query := url.Values{"nonce": {strconv.FormatInt(nonce, 10)}}
	for name, value := range params {
		body[name] = value
		query.Set(name, fmt.Sprint(value))
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("max payload: %w", err)
	}
	payload := base64.StdEncoding.EncodeToString(raw)
	mac := hmac.New(sha256.New, []byte(m.secretKey))
	mac.Write([]byte(payload))

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, m.baseURL+path+"?"+query.Encode(), nil)
	if err != nil {
		return fmt.Errorf("max %s: %w", path, err)
	}
	request.Header.Set("X-MAX-ACCESSKEY", m.accessKey)
	request.Header.Set("X-MAX-PAYLOAD", payload)
	request.Header.Set("X-MAX-SIGNATURE", hex.EncodeToString(mac.Sum(nil)))
	if err := get(m.client, request, into); err != nil {
		return fmt.Errorf("max: %w", err)
	}
	return nil
}
