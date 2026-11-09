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
	"strconv"
	"strings"
	"time"
)

// MAX (MaiCoin) holds coins in its spot wallet. It prints no total of its own, so its valuation is the
// wallet at MAX's own markets in NT$, as InTheGreenYet worked it out.
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
	if err := m.signed(ctx, "/api/v3/wallet/spot/accounts", &accounts); err != nil {
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
	}
	if priced {
		snapshot.Valuation = &Value{Unit: "TWD", Amount: worth}
	}
	return snapshot, nil
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

// signed sends a GET to a private endpoint. MAX signs the base64 of a JSON payload carrying the path and
// a nonce, and wants the signature in hex.
func (m *MAX) signed(ctx context.Context, path string, into any) error {
	nonce := time.Now().UnixMilli()
	raw, err := json.Marshal(struct {
		Nonce int64  `json:"nonce"`
		Path  string `json:"path"`
	}{nonce, path})
	if err != nil {
		return fmt.Errorf("max payload: %w", err)
	}
	payload := base64.StdEncoding.EncodeToString(raw)
	mac := hmac.New(sha256.New, []byte(m.secretKey))
	mac.Write([]byte(payload))

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, m.baseURL+path+"?nonce="+strconv.FormatInt(nonce, 10), nil)
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
