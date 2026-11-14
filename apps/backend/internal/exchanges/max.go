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
	return snapshot, nil
}

type maxDeposit struct {
	UUID     string  `json:"uuid"`
	Currency string  `json:"currency"`
	Amount   decimal `json:"amount"`
	// NT$'s: the virtual account the money was sent to.
	ToAddress string `json:"to_address"`
	TxID      string `json:"txid"`
	CreatedAt int64  `json:"created_at"`
	State     string `json:"state"`
}

type maxWithdrawal struct {
	UUID        string  `json:"uuid"`
	Currency    string  `json:"currency"`
	Amount      decimal `json:"amount"`
	Fee         decimal `json:"fee"`
	FeeCurrency string  `json:"fee_currency"`
	TxID        string  `json:"txid"`
	CreatedAt   int64   `json:"created_at"`
	State       string  `json:"state"`
}

// maxHistory is how many of each MAX lists at once: far more than a month of a person's.
const maxHistory = 1000

// maxSettled says whether MAX is done with a deposit or withdrawal (done), gave up on it (failed,
// canceled, rejected), or may still settle it.
func maxSettled(state string) (done, pending bool) {
	switch state {
	case "done":
		return true, false
	case "failed", "canceled", "cancelled", "rejected":
		return false, false
	default:
		return false, true
	}
}

// Movements are MAX's deposits and withdrawals created in the span, NT$ and coins alike, oldest first.
func (m *MAX) Movements(ctx context.Context, from, to time.Time) ([]Movement, error) {
	params := map[string]any{"order": "asc", "timestamp": from.UnixMilli(), "limit": maxHistory}
	var deposits []maxDeposit
	if err := m.signed(ctx, "/api/v3/deposits", params, &deposits); err != nil {
		return nil, err
	}
	var withdrawals []maxWithdrawal
	if err := m.signed(ctx, "/api/v3/withdrawals", params, &withdrawals); err != nil {
		return nil, err
	}
	within := func(ms int64) bool { at := time.UnixMilli(ms); return !at.Before(from) && at.Before(to) }
	var moves []Movement
	for _, d := range deposits {
		done, pending := maxSettled(d.State)
		if !within(d.CreatedAt) || (!done && !pending) {
			continue
		}
		amount, err := quantity(string(d.Amount))
		if err != nil {
			return nil, fmt.Errorf("max deposit %s: %w", d.UUID, err)
		}
		move := Movement{ID: "deposit/" + d.UUID, At: time.UnixMilli(d.CreatedAt), Unit: strings.ToUpper(d.Currency), Amount: amount, TxID: d.TxID, Pending: pending}
		if strings.EqualFold(d.Currency, "twd") {
			move.TxID, move.Memo = "", d.ToAddress
		}
		moves = append(moves, move)
	}
	for _, w := range withdrawals {
		done, pending := maxSettled(w.State)
		if !within(w.CreatedAt) || (!done && !pending) {
			continue
		}
		amount, err := quantity(string(w.Amount))
		if err != nil {
			return nil, fmt.Errorf("max withdrawal %s: %w", w.UUID, err)
		}
		fee, err := quantity(string(w.Fee))
		if err != nil {
			return nil, fmt.Errorf("max withdrawal %s fee: %w", w.UUID, err)
		}
		move := Movement{
			ID: "withdrawal/" + w.UUID, At: time.UnixMilli(w.CreatedAt), Unit: strings.ToUpper(w.Currency), Amount: amount.Neg(amount),
			Fee: fee, FeeUnit: strings.ToUpper(w.FeeCurrency), TxID: w.TxID, Pending: pending,
		}
		if strings.EqualFold(w.Currency, "twd") {
			move.TxID = ""
		}
		moves = append(moves, move)
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
