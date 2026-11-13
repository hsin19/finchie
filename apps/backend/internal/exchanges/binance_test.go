package exchanges

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// fakeFiatOrders serves Binance's fiat orders a page at a time, deposits (transactionType 0) and
// withdrawals (1); it refuses a request not signed with the secret.
func fakeFiatOrders(t *testing.T, deposits, withdrawals []binanceFiatOrder) *Binance {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sapi/v1/fiat/orders" {
			http.NotFound(w, r)
			return
		}
		raw := r.URL.RawQuery
		cut := strings.LastIndex(raw, "&signature=")
		mac := hmac.New(sha256.New, []byte("secret"))
		mac.Write([]byte(raw[:max(cut, 0)]))
		if cut < 0 || r.Header.Get("X-MBX-APIKEY") != "key" || raw[cut+len("&signature="):] != hex.EncodeToString(mac.Sum(nil)) ||
			r.URL.Query().Get("beginTime") == "" || r.URL.Query().Get("endTime") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"code": -1022, "msg": "Signature for this request is not valid."}`))
			return
		}
		orders := map[string][]binanceFiatOrder{"0": deposits, "1": withdrawals}[r.URL.Query().Get("transactionType")]
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		rows, _ := strconv.Atoi(r.URL.Query().Get("rows"))
		from, to := min((page-1)*rows, len(orders)), min(page*rows, len(orders))
		_ = json.NewEncoder(w).Encode(map[string]any{"code": "000000", "data": orders[from:to], "total": len(orders), "success": true})
	}))
	t.Cleanup(server.Close)
	b := NewBinance("key", "secret", server.Client())
	b.baseURL = server.URL
	return b
}

func TestBinanceReadsTheFiatOrdersThatWentThroughAPageAtATime(t *testing.T) {
	// A page and one more of deposits: the last one is on the second page.
	var deposits []binanceFiatOrder
	for i := range binancePage {
		deposits = append(deposits, binanceFiatOrder{OrderNo: fmt.Sprintf("old-%d", i), FiatCurrency: "USD", IndicatedAmount: "1", Amount: "1", TotalFee: "0", Status: "Failed"})
	}
	deposits = append(deposits, binanceFiatOrder{
		OrderNo: "d1", FiatCurrency: "USD", IndicatedAmount: "1585", Amount: "1580", TotalFee: "5",
		Method: "Bank Transfer (SWIFT)", Status: "Successful", CreateTime: 1772510760000,
	})
	withdrawals := []binanceFiatOrder{{
		OrderNo: "w1", FiatCurrency: "USD", IndicatedAmount: "500", Amount: "485", TotalFee: "15",
		Method: "Bank Transfer (SWIFT)", Status: "Finished", CreateTime: 1772510760000,
	}}
	moves, err := fakeFiatOrders(t, deposits, withdrawals).movements(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, m := range moves {
		got = append(got, fmt.Sprintf("%s %s %s %s", m.ID, m.Unit, m.Amount.RatString(), m.Memo))
	}
	// What the bank sent, the fee taken after; what the bank was sent, the fee taken before.
	want := []string{
		"d1 USD 1585 Bank Transfer (SWIFT) · 手續費 5 USD",
		"w1 USD -485 Bank Transfer (SWIFT) · 手續費 15 USD",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("movements =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
