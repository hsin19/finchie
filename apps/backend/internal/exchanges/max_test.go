package exchanges

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"maps"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeMAX serves the spot wallet and the tickers; it refuses a request not signed with the secret.
func fakeMAX(t *testing.T, accounts string) *MAX {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/tickers":
			_, _ = w.Write([]byte(`{
				"btctwd": {"at": 1794200000, "last": "3000000.0", "vol": "1.2"},
				"maxusdt": {"last": "0.5"},
				"usdttwd": {"last": "32.0"},
				"oldtwd": {"last": "0"}
			}`))
		case "/api/v3/wallet/spot/accounts":
			payload := r.Header.Get("X-MAX-PAYLOAD")
			mac := hmac.New(sha256.New, []byte("secret"))
			mac.Write([]byte(payload))
			raw, _ := base64.StdEncoding.DecodeString(payload)
			var body struct {
				Nonce int64  `json:"nonce"`
				Path  string `json:"path"`
			}
			if r.Header.Get("X-MAX-ACCESSKEY") != "key" || r.Header.Get("X-MAX-SIGNATURE") != hex.EncodeToString(mac.Sum(nil)) ||
				json.Unmarshal(raw, &body) != nil || body.Path != r.URL.Path || r.URL.Query().Get("nonce") == "" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"success": false, "error": {"code": 2006, "message": "wrong signature"}}`))
				return
			}
			_, _ = w.Write([]byte(accounts))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	m := NewMAX("key", "secret", server.Client())
	m.baseURL = server.URL
	return m
}

func TestMAXHoldsItsSpotWalletWorthItsOwnMarketsInNTD(t *testing.T) {
	m := fakeMAX(t, `[
		{"currency": "twd", "balance": "1000", "locked": "0", "staked": null},
		{"currency": "btc", "balance": "0.1", "locked": "0.05", "staked": "0"},
		{"currency": "max", "balance": "10", "locked": "0", "staked": "5"},
		{"currency": "usdt", "balance": "100", "locked": "0", "staked": null},
		{"currency": "eth", "balance": "0", "locked": "0", "staked": null}
	]`)
	snapshot, err := m.Fetch(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"TWD": "1000", "BTC": "0.15", "MAX": "15", "USDT": "100"}
	if len(snapshot.Holdings) != len(want) {
		t.Fatalf("holdings = %v", snapshot.Holdings)
	}
	for unit, amount := range want {
		if got := snapshot.Holdings[unit]; got == nil || got.FloatString(2) != mustRat(amount).FloatString(2) {
			t.Fatalf("%s = %v, want %s", unit, got, amount)
		}
	}
	// 1000 + 0.15 × 3,000,000 + 15 × 0.5 × 32 + 100 × 32.
	if v := snapshot.Valuation; v == nil || v.Unit != "TWD" || v.Amount.FloatString(2) != "454440.00" {
		t.Fatalf("valuation = %+v", v)
	}
	// Each coin's price in NT$, at its TWD market or its USDT one; NT$ has none.
	assertPrices(t, snapshot, map[string]string{"BTC": "3000000 TWD", "MAX": "16 TWD", "USDT": "32 TWD"})
}

func assertPrices(t *testing.T, snapshot Snapshot, want map[string]string) {
	t.Helper()
	got := map[string]string{}
	for coin, p := range snapshot.Prices {
		got[coin] = p.Amount.RatString() + " " + p.Unit
	}
	if !maps.Equal(got, want) {
		t.Fatalf("prices = %v, want %v", got, want)
	}
}

func TestMAXGivesNoValuationWhenACoinHasNoMarketToPriceIt(t *testing.T) {
	m := fakeMAX(t, `[{"currency": "twd", "balance": "1000", "locked": "0"}, {"currency": "old", "balance": "3", "locked": "0"}]`)
	snapshot, err := m.Fetch(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Valuation != nil || snapshot.Holdings["OLD"] == nil {
		t.Fatalf("snapshot = %+v", snapshot)
	}
}

func TestMAXRefusingTheKeyIsAnErrorThatSaysWhy(t *testing.T) {
	m := fakeMAX(t, `[]`)
	m.secretKey = "wrong"
	_, err := m.Fetch(t.Context())
	if err == nil || !strings.Contains(err.Error(), "wrong signature") {
		t.Fatalf("err = %v", err)
	}
}

func mustRat(s string) *big.Rat {
	r, err := quantity(s)
	if err != nil {
		panic(err)
	}
	return r
}
