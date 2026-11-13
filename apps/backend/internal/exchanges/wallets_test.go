package exchanges

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// assertSnapshot compares holdings and the valuation, each to two places.
func assertSnapshot(t *testing.T, snapshot Snapshot, holdings map[string]string, valuation string) {
	t.Helper()
	if len(snapshot.Holdings) != len(holdings) {
		t.Fatalf("holdings = %v, want %v", snapshot.Holdings, holdings)
	}
	for unit, amount := range holdings {
		if got := snapshot.Holdings[unit]; got == nil || got.FloatString(2) != mustRat(amount).FloatString(2) {
			t.Fatalf("%s = %v, want %s (holdings %v)", unit, got, amount, snapshot.Holdings)
		}
	}
	if v := snapshot.Valuation; v == nil || v.Unit != "USDT" || v.Amount.FloatString(2) != mustRat(valuation).FloatString(2) {
		t.Fatalf("valuation = %+v, want %s USDT", v, valuation)
	}
}

// Spot and funding list coins that add up to their values. Earn lists none. Margin's coins come to more
// than its value: what is borrowed is not taken off, so its coins are not what the account holds.
const binanceWallets = `[
	{"activate": true, "walletName": "Spot", "balance": "9100", "assetBalances": [
		{"asset": "BTC", "free": "0.1", "locked": "0.05", "freeze": "0", "withdrawing": "0"},
		{"asset": "USDT", "free": "100", "locked": "0", "freeze": "0", "withdrawing": "0"},
		{"asset": "DUST", "free": "3", "locked": "0", "freeze": "0", "withdrawing": "0"}
	]},
	{"activate": true, "walletName": "Funding", "balance": "50.2", "assetBalances": [
		{"asset": "USDT", "free": "40", "locked": "0", "freeze": "10", "withdrawing": "0"}
	]},
	{"activate": true, "walletName": "Earn", "balance": "300", "assetBalances": []},
	{"activate": true, "walletName": "Cross Margin", "balance": "10", "assetBalances": [
		{"asset": "BTC", "free": "1", "locked": "0", "freeze": "0", "withdrawing": "0"}
	]},
	{"activate": true, "walletName": "USDⓈ-M Futures", "balance": "1000"},
	{"activate": false, "walletName": "Options", "balance": "0"}
]`

func TestBinanceListsAWalletByCoinOnlyWhenItsCoinsAddUpToItsValue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/ticker/price":
			_, _ = w.Write([]byte(`[{"symbol": "BTCUSDT", "price": "60000.00"}, {"symbol": "ETHBTC", "price": "0.05"}]`))
		case "/sapi/v1/asset/wallet/balance":
			query := r.URL.RawQuery
			signed, signature, _ := strings.Cut(query, "&signature=")
			mac := hmac.New(sha256.New, []byte("secret"))
			mac.Write([]byte(signed))
			if r.Header.Get("X-MBX-APIKEY") != "key" || signature != hex.EncodeToString(mac.Sum(nil)) ||
				r.URL.Query().Get("needBalanceDetail") != "true" || r.URL.Query().Get("quoteAsset") != "USDT" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"code": -1022, "msg": "Signature for this request is not valid."}`))
				return
			}
			_, _ = w.Write([]byte(binanceWallets))
		case "/sapi/v1/fiat/orders":
			_, _ = w.Write([]byte(`{"code": "000000", "data": [], "total": 0, "success": true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	b := NewBinance("key", "secret", server.Client())
	b.baseURL = server.URL

	snapshot, err := b.Fetch(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	// USDT: spot 100, funding 50, then earn 300, margin 10, and futures 1000 by their values.
	assertSnapshot(t, snapshot, map[string]string{"BTC": "0.15", "DUST": "3", "USDT": "1460"}, "10460.2")
	// Prices for the coins held that a market prices: not DUST, nor USDT in itself.
	assertPrices(t, snapshot, map[string]string{"BTC": "60000 USDT"})

	b.apiSecret = "wrong"
	if _, err := b.Fetch(t.Context()); err == nil || !strings.Contains(err.Error(), "-1022") {
		t.Fatalf("a refused key: err = %v", err)
	}
}

func TestBitgetListsSpotFundingAndEarnByCoinAndTheRestByValue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v2/spot/market/tickers" {
			_, _ = w.Write([]byte(`{"code": "00000", "msg": "success", "data": [
				{"symbol": "BTCUSDT", "lastPr": "60000"}, {"symbol": "BGBUSDT", "lastPr": "1.5"}]}`))
			return
		}
		mac := hmac.New(sha256.New, []byte("secret"))
		mac.Write([]byte(r.Header.Get("ACCESS-TIMESTAMP") + r.Method + r.URL.RequestURI()))
		if r.Header.Get("ACCESS-KEY") != "key" || r.Header.Get("ACCESS-PASSPHRASE") != "pass" ||
			r.Header.Get("ACCESS-SIGN") != base64.StdEncoding.EncodeToString(mac.Sum(nil)) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"code": "40009", "msg": "sign signature error", "data": null}`))
			return
		}
		data := map[string]string{
			"/api/v2/account/all-account-balance": `[{"accountType": "spot", "usdtBalance": "6150"}, {"accountType": "funding", "usdtBalance": "20"},
				{"accountType": "earn", "usdtBalance": "150"}, {"accountType": "futures", "usdtBalance": "500"}, {"accountType": "bots", "usdtBalance": "0"}]`,
			"/api/v2/spot/account/assets": `[{"coin": "BTC", "available": "0.1", "frozen": "0", "locked": "0", "limitAvailable": "0"},
				{"coin": "BGB", "available": "100", "frozen": "0", "locked": "0"}]`,
			"/api/v2/account/funding-assets": `[{"coin": "USDT", "available": "20", "frozen": "0", "usdtValue": "20"}]`,
			"/api/v2/earn/account/assets":    `[{"coin": "BGB", "amount": "100"}]`,
		}[r.URL.Path]
		if data == "" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"code": "00000", "msg": "success", "requestTime": 1794200000000, "data": ` + data + `}`))
	}))
	defer server.Close()
	g := NewBitget("key", "secret", "pass", server.Client())
	g.baseURL = server.URL

	snapshot, err := g.Fetch(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	// BGB: spot 100 and earn 100. USDT: funding 20 and futures 500.
	assertSnapshot(t, snapshot, map[string]string{"BTC": "0.1", "BGB": "200", "USDT": "520"}, "6820")

	g.passphrase = "wrong"
	if _, err := g.Fetch(t.Context()); err == nil || !strings.Contains(err.Error(), "40009") {
		t.Fatalf("a refused key: err = %v", err)
	}
}

func TestPricesComeFromAUSDTMarketElseABTCOne(t *testing.T) {
	prices := usdtPrices(map[string]*big.Rat{"BTCUSDT": mustRat("60000"), "ETHBTC": mustRat("0.05"), "ETHUSDT": mustRat("3001"), "XYZBTC": mustRat("0.0001")})
	for coin, want := range map[string]string{"USDT": "1", "BTC": "60000", "ETH": "3001", "XYZ": "6"} {
		if got := prices[coin]; got == nil || got.FloatString(2) != mustRat(want).FloatString(2) {
			t.Fatalf("%s = %v, want %s", coin, got, want)
		}
	}
}
