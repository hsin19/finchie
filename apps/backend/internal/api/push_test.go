package api

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hsin19/finchie/apps/backend/internal/domain"
	"github.com/hsin19/finchie/apps/backend/internal/notify"
	"github.com/hsin19/finchie/apps/backend/internal/push"
)

func TestADeviceSubscribesWithTheDeploymentsKeyAndATestPushReachesIt(t *testing.T) {
	e, _ := newAPI(t, 1<<20)
	var received atomic.Int32
	service := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		received.Add(1)
		w.WriteHeader(http.StatusCreated)
	}))
	defer service.Close()
	previous := push.Client
	push.Client = service.Client()
	defer func() { push.Client = previous }()

	settings := decode[Settings](t, get(t, e, "/api/settings"))
	if settings.PushPublicKey == "" || decode[Settings](t, get(t, e, "/api/settings")).PushPublicKey != settings.PushPublicKey {
		t.Fatalf("pushPublicKey = %q, want one kept", settings.PushPublicKey)
	}

	key, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	encode := base64.RawURLEncoding.EncodeToString
	subscription := fmt.Sprintf(`{"endpoint": %q, "keys": {"p256dh": %q, "auth": %q}}`, service.URL+"/device", encode(key.PublicKey().Bytes()), encode(make([]byte, 16)))
	if got := postJSON(t, e, "/api/push/subscribe", subscription); got.Code != http.StatusNoContent {
		t.Fatalf("subscribe: %d %s", got.Code, got.Body)
	}
	if got := postJSON(t, e, "/api/push/subscribe", `{"endpoint": "http://push.example/1", "keys": {"p256dh": "k", "auth": "a"}}`); got.Code != http.StatusBadRequest {
		t.Fatalf("subscribe over http: %d", got.Code)
	}

	if got := decode[PushSent](t, postJSON(t, e, "/api/push/test", "")); got.Sent != 1 || received.Load() != 1 {
		t.Fatalf("test push sent %d, received %d; want 1", got.Sent, received.Load())
	}
	if got := postJSON(t, e, "/api/push/unsubscribe", fmt.Sprintf(`{"endpoint": %q}`, service.URL+"/device")); got.Code != http.StatusNoContent {
		t.Fatalf("unsubscribe: %d", got.Code)
	}
	if got := decode[PushSent](t, postJSON(t, e, "/api/push/test", "")); got.Sent != 0 {
		t.Fatalf("after unsubscribing, sent %d", got.Sent)
	}
}

func TestAPushCarriesTheNotificationAsTheAPIShowsIt(t *testing.T) {
	label := "臺幣活存"
	message := notify.Message{Base: "TWD", Notification: &notify.Pushed{
		ID: 7, Kind: domain.NoticeShortfall, RaisedAt: time.Date(2026, 11, 16, 6, 0, 0, 0, time.UTC),
		Account: &notify.Account{ID: 3, Institution: "esun", Kind: domain.Deposit, Label: &label},
		Facts: notify.Facts{Shortfall: &notify.Shortfall{
			Unit: "TWD", AsOf: "2026-10-31", Balance: "5000", Owed: "7000", Lacks: "2000",
			Debits: []notify.Debit{{StatementID: 9, Institution: "esun", DueDate: "2026-11-23", Unit: "TWD", Amount: "7000"}},
		}},
	}}
	encoded, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	var shown struct {
		Base         string       `json:"base"`
		Notification Notification `json:"notification"`
	}
	if err := json.Unmarshal(encoded, &shown); err != nil {
		t.Fatal(err)
	}
	n := shown.Notification
	if shown.Base != "TWD" || n.Id != 7 || n.Kind != NotificationKindShortfall || n.Account == nil || *n.Account.Label != label ||
		n.Shortfall == nil || n.Shortfall.Lacks != "2000" || n.Shortfall.Debits[0].DueDate.String() != "2026-11-23" {
		t.Fatalf("shown = %+v from %s", shown, encoded)
	}
}
