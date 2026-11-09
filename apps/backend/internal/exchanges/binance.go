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
// gives both the valuation and the holdings.
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
