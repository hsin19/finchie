package exchanges

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"math/big"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"
)

// fakeMAX serves the spot wallet, the tickers, and the NT$ deposits and withdrawals (none unless given);
// it refuses a request not signed with the secret, or whose signed payload and query disagree.
func fakeMAX(t *testing.T, accounts string, movements ...string) *MAX {
	t.Helper()
	deposits, withdrawals := "[]", "[]"
	if len(movements) == 2 {
		deposits, withdrawals = movements[0], movements[1]
	}
	private := map[string]string{"/api/v3/wallet/spot/accounts": accounts, "/api/v3/deposits": deposits, "/api/v3/withdrawals": withdrawals}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v2/tickers" {
			_, _ = w.Write([]byte(`{
				"btctwd": {"at": 1794200000, "last": "3000000.0", "vol": "1.2"},
				"maxusdt": {"last": "0.5"},
				"usdttwd": {"last": "32.0"},
				"oldtwd": {"last": "0"}
			}`))
			return
		}
		reply, ok := private[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		payload := r.Header.Get("X-MAX-PAYLOAD")
		mac := hmac.New(sha256.New, []byte("secret"))
		mac.Write([]byte(payload))
		raw, _ := base64.StdEncoding.DecodeString(payload)
		var body map[string]any
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber() // a nonce as it was sent, not as a float
		signed := decoder.Decode(&body) == nil && body["path"] == r.URL.Path && r.URL.Query().Get("nonce") != ""
		for name, values := range r.URL.Query() {
			signed = signed && fmt.Sprint(body[name]) == values[0]
		}
		if r.Header.Get("X-MAX-ACCESSKEY") != "key" || r.Header.Get("X-MAX-SIGNATURE") != hex.EncodeToString(mac.Sum(nil)) || !signed {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"success": false, "error": {"code": 2006, "message": "wrong signature"}}`))
			return
		}
		// History is read oldest first from a moment on.
		if r.URL.Path != "/api/v3/wallet/spot/accounts" && (r.URL.Query().Get("order") != "asc" || r.URL.Query().Get("timestamp") == "") {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(reply))
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

// movementRows shows movements as "id time unit amount (fee unit) txid memo pending".
func movementRows(moves []Movement) []string {
	var rows []string
	for _, m := range moves {
		fee := ""
		if m.Fee != nil && m.Fee.Sign() != 0 {
			fee = " fee " + m.Fee.RatString() + " " + m.FeeUnit
		}
		rows = append(rows, fmt.Sprintf("%s %s %s %s%s tx=%s memo=%s pending=%v", m.ID, m.At.UTC().Format("2006-01-02 15:04"), m.Unit, m.Amount.RatString(), fee, m.TxID, m.Memo, m.Pending))
	}
	return rows
}

func TestMAXReadsItsDepositsAndWithdrawalsOfEveryCurrencyWithinASpan(t *testing.T) {
	m := fakeMAX(t, `[]`, `[
		{"uuid": "d0", "currency": "twd", "amount": "1.0", "created_at": 1767225599000, "state": "done"},
		{"uuid": "d1", "currency": "twd", "network_protocol": "fiat", "amount": "100000.0", "to_address": "12345678901123", "txid": "", "created_at": 1768972020000, "state": "done"},
		{"uuid": "d2", "currency": "twd", "amount": "5000.0", "to_address": "12345678901123", "created_at": 1768972080000, "state": "processing"},
		{"uuid": "d3", "currency": "usdt", "amount": "10.0", "txid": "0xaaa", "created_at": 1768972080000, "state": "done"}
	]`, `[
		{"uuid": "w1", "currency": "twd", "amount": "30000.0", "fee": "15.0", "fee_currency": "twd", "created_at": 1769000000000, "state": "done", "transaction_type": "external"},
		{"uuid": "w2", "currency": "twd", "amount": "1000.0", "fee": "0", "fee_currency": "twd", "created_at": 1769000000000, "state": "failed"},
		{"uuid": "w3", "currency": "usdt", "amount": "3177.13", "fee": "0.0", "fee_currency": "max", "txid": "0x35ab45a3", "created_at": 1769429160000, "state": "done"},
		{"uuid": "w4", "currency": "usdt", "amount": "5.0", "fee": "0", "fee_currency": "usdt", "txid": "0xbbb", "created_at": 1769904000000, "state": "done"}
	]`)
	// January 2026 in UTC: d0 the moment before it, w4 the first of February.
	moves, err := m.Movements(t.Context(), time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"deposit/d1 2026-01-21 05:07 TWD 100000 tx= memo=12345678901123 pending=false",
		"deposit/d2 2026-01-21 05:08 TWD 5000 tx= memo=12345678901123 pending=true",
		"deposit/d3 2026-01-21 05:08 USDT 10 tx=0xaaa memo= pending=false",
		"withdrawal/w1 2026-01-21 12:53 TWD -30000 fee 15 TWD tx= memo= pending=false",
		"withdrawal/w3 2026-01-26 12:06 USDT -317713/100 tx=0x35ab45a3 memo= pending=false",
	}
	if got := movementRows(moves); !slices.Equal(got, want) {
		t.Fatalf("movements =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
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
