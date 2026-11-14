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
	"time"
)

// fakeBinanceHistory serves Binance's fiat orders a page at a time (deposits are transactionType 0,
// withdrawals 1) and its coins' deposits and withdrawals; it refuses a request not signed with the secret,
// or one without its span.
func fakeBinanceHistory(t *testing.T, fiatDeposits, fiatWithdrawals []binanceFiatOrder, deposits, withdrawals string) *Binance {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := r.URL.RawQuery
		cut := strings.LastIndex(raw, "&signature=")
		mac := hmac.New(sha256.New, []byte("secret"))
		mac.Write([]byte(raw[:max(cut, 0)]))
		if cut < 0 || r.Header.Get("X-MBX-APIKEY") != "key" || raw[cut+len("&signature="):] != hex.EncodeToString(mac.Sum(nil)) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"code": -1022, "msg": "Signature for this request is not valid."}`))
			return
		}
		q := r.URL.Query()
		switch r.URL.Path {
		case "/sapi/v1/fiat/orders":
			if q.Get("beginTime") == "" || q.Get("endTime") == "" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			orders := map[string][]binanceFiatOrder{"0": fiatDeposits, "1": fiatWithdrawals}[q.Get("transactionType")]
			page, _ := strconv.Atoi(q.Get("page"))
			rows, _ := strconv.Atoi(q.Get("rows"))
			from, to := min((page-1)*rows, len(orders)), min(page*rows, len(orders))
			_ = json.NewEncoder(w).Encode(map[string]any{"code": "000000", "data": orders[from:to], "total": len(orders), "success": true})
		case "/sapi/v1/capital/deposit/hisrec", "/sapi/v1/capital/withdraw/history":
			if q.Get("startTime") == "" || q.Get("endTime") == "" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte(map[string]string{"/sapi/v1/capital/deposit/hisrec": deposits, "/sapi/v1/capital/withdraw/history": withdrawals}[r.URL.Path]))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	b := NewBinance("key", "secret", server.Client())
	b.baseURL = server.URL
	return b
}

func TestBinanceReadsItsFiatOrdersAPageAtATimeAndItsCoinsMovements(t *testing.T) {
	// A page and one more of fiat deposits: the last one is on the second page.
	var fiat []binanceFiatOrder
	for i := range binancePage {
		fiat = append(fiat, binanceFiatOrder{OrderNo: fmt.Sprintf("old-%d", i), FiatCurrency: "USD", IndicatedAmount: "1", Amount: "1", TotalFee: "0", Status: "Failed"})
	}
	fiat = append(fiat, binanceFiatOrder{
		OrderNo: "d1", FiatCurrency: "USD", IndicatedAmount: "1585", Amount: "1580", TotalFee: "5",
		Method: "Bank Transfer (SWIFT)", Status: "Successful", CreateTime: 1772510760000,
	}, binanceFiatOrder{OrderNo: "d2", FiatCurrency: "USD", IndicatedAmount: "100", Amount: "100", TotalFee: "0", Method: "Bank Transfer (SWIFT)", Status: "Processing", CreateTime: 1772510760000})
	withdrawals := []binanceFiatOrder{{
		OrderNo: "w1", FiatCurrency: "USD", IndicatedAmount: "500", Amount: "485", TotalFee: "15",
		Method: "Bank Transfer (SWIFT)", Status: "Finished", CreateTime: 1772510760000,
	}}
	b := fakeBinanceHistory(t, fiat, withdrawals, `[
		{"id": "c1", "coin": "USDT", "amount": "3153.3", "status": 1, "txId": "0xeee4b23f", "insertTime": 1771859400000},
		{"id": "c2", "coin": "U", "amount": "7.52", "status": 0, "txId": "0x6de817d4", "insertTime": 1772805060000},
		{"id": "c3", "coin": "U", "amount": "1", "status": 7, "txId": "0x0", "insertTime": 1772805060000}
	]`, `[
		{"id": "x1", "coin": "U", "amount": "100", "transactionFee": "0.5", "status": 6, "txId": "0xe01c7d68", "applyTime": "2026-02-16 11:51:00"},
		{"id": "x2", "coin": "U", "amount": "50", "transactionFee": "0", "status": 3, "txId": "", "applyTime": "2026-02-17 11:51:00"}
	]`)
	moves, err := b.Movements(t.Context(), time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	// What the bank sent, the fee taken after; what the bank was sent, the fee taken before.
	want := []string{
		"d1 2026-03-03 04:06 USD 1585 fee 5 USD tx= memo=Bank Transfer (SWIFT) pending=false",
		"d2 2026-03-03 04:06 USD 100 tx= memo=Bank Transfer (SWIFT) pending=true",
		"w1 2026-03-03 04:06 USD -485 fee 15 USD tx= memo=Bank Transfer (SWIFT) pending=false",
		"deposit/c1 2026-02-23 15:10 USDT 31533/10 tx=0xeee4b23f memo= pending=false",
		"deposit/c2 2026-03-06 13:51 U 188/25 tx=0x6de817d4 memo= pending=true",
		"withdrawal/x1 2026-02-16 11:51 U -100 fee 1/2 U tx=0xe01c7d68 memo= pending=false",
	}
	if got := movementRows(moves); !slices.Equal(got, want) {
		t.Fatalf("movements =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
