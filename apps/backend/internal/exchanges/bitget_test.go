package exchanges

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"
)

// fakeBitgetHistory serves Bitget's deposits and withdrawals, newest first, a page at a time below
// idLessThan; it refuses a request not signed with the secret, or one without its span, as Bitget does.
func fakeBitgetHistory(t *testing.T, deposits, withdrawals []bitgetMovement) *Bitget {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mac := hmac.New(sha256.New, []byte("secret"))
		mac.Write([]byte(r.Header.Get("ACCESS-TIMESTAMP") + r.Method + r.URL.RequestURI()))
		q := r.URL.Query()
		if r.Header.Get("ACCESS-KEY") != "key" || r.Header.Get("ACCESS-PASSPHRASE") != "phrase" ||
			r.Header.Get("ACCESS-SIGN") != base64.StdEncoding.EncodeToString(mac.Sum(nil)) || q.Get("startTime") == "" || q.Get("endTime") == "" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"code": "400172", "msg": "Parameter verification failed"}`))
			return
		}
		all := map[string][]bitgetMovement{"/api/v2/spot/wallet/deposit-records": deposits, "/api/v2/spot/wallet/withdrawal-records": withdrawals}[r.URL.Path]
		rows := all
		if before := q.Get("idLessThan"); before != "" {
			i := slices.IndexFunc(all, func(m bitgetMovement) bool { return m.OrderID == before })
			rows = all[i+1:]
		}
		rows = rows[:min(len(rows), bitgetPage)]
		var data any = rows
		if len(rows) == 0 {
			data = nil // Bitget's "no records"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": "00000", "msg": "success", "data": data})
	}))
	t.Cleanup(server.Close)
	g := NewBitget("key", "secret", "phrase", server.Client())
	g.baseURL = server.URL
	return g
}

func TestBitgetReadsItsDepositsAndWithdrawalsAPageAtATime(t *testing.T) {
	// A page of failed deposits and one more: the last one is on the second page.
	var deposits []bitgetMovement
	for i := range bitgetPage {
		deposits = append(deposits, bitgetMovement{OrderID: fmt.Sprintf("f%d", i), Coin: "USDT", Size: "1", Status: "fail", CTime: "1769429220000"})
	}
	deposits = append(deposits, bitgetMovement{OrderID: "d1", TradeID: "0x35ab45a3", Coin: "USDT", Size: "3177.13", Status: "success", CTime: "1769429220000"})
	withdrawals := []bitgetMovement{
		{OrderID: "w1", TradeID: "0x69bfd941", Coin: "ETH", Size: "0.00999", Fee: "-0.0002", Status: "success", CTime: "1767368700000"},
		{OrderID: "w2", TradeID: "", Coin: "ETH", Size: "1", Fee: "-0.0002", Status: "pending", CTime: "1767368700000"},
	}
	moves, err := fakeBitgetHistory(t, deposits, withdrawals).Movements(t.Context(), time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"deposit/d1 2026-01-26 12:07 USDT 317713/100 tx=0x35ab45a3 memo= pending=false",
		"withdrawal/w1 2026-01-02 15:45 ETH -999/100000 fee 1/5000 ETH tx=0x69bfd941 memo= pending=false",
		"withdrawal/w2 2026-01-02 15:45 ETH -1 fee 1/5000 ETH tx= memo= pending=true",
	}
	if got := movementRows(moves); !slices.Equal(got, want) {
		t.Fatalf("movements =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	// None at all is no error.
	if moves, err := fakeBitgetHistory(t, nil, nil).Movements(t.Context(), time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)); err != nil || len(moves) != 0 {
		t.Fatalf("none: %v, %v", moves, err)
	}
}
