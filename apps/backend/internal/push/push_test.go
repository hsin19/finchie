package push_test

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hsin19/finchie/apps/backend/internal/db/dbq"
	"github.com/hsin19/finchie/apps/backend/internal/push"
	"github.com/hsin19/finchie/apps/backend/internal/testdb"
)

// PushService stands in for a browser's push service: it takes what is sent to its endpoints, and
// answers each path with the status set for it, 201 by default.
type PushService struct {
	*httptest.Server
	mu       sync.Mutex
	Received []*http.Request
	Status   map[string]int
}

func NewPushService(t *testing.T) *PushService {
	t.Helper()
	s := &PushService{Status: map[string]int{}}
	s.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.Received = append(s.Received, r)
		status, ok := s.Status[r.URL.Path]
		if !ok {
			status = http.StatusCreated
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(s.Close)
	previous := push.Client
	push.Client = s.Client()
	t.Cleanup(func() { push.Client = previous })
	return s
}

// Device subscribes with keys of its own, as a browser does, at path on the push service.
func Device(t *testing.T, pool *pgxpool.Pool, service *PushService, path string) {
	t.Helper()
	key, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	auth := make([]byte, 16)
	if _, err := rand.Read(auth); err != nil {
		t.Fatal(err)
	}
	encode := base64.RawURLEncoding.EncodeToString
	if err := push.Subscribe(t.Context(), dbq.New(pool), service.URL+path, encode(key.PublicKey().Bytes()), encode(auth)); err != nil {
		t.Fatal(err)
	}
}

func TestTheKeyPairIsMadeOnceAndKept(t *testing.T) {
	queries := dbq.New(testdb.Pool(t))
	first, err := push.Keys(t.Context(), queries)
	if err != nil {
		t.Fatal(err)
	}
	again, err := push.Keys(t.Context(), queries)
	if err != nil {
		t.Fatal(err)
	}
	if first.PublicKey == "" || first != again {
		t.Fatalf("keys = %+v then %+v, want one pair kept", first, again)
	}
}

func TestEachMessageGoesToEveryDeviceEncryptedAndSigned(t *testing.T) {
	pool := testdb.Pool(t)
	service := NewPushService(t)
	Device(t, pool, service, "/phone")
	Device(t, pool, service, "/desktop")

	sent, err := push.Send(t.Context(), pool, []byte(`{"base":"TWD"}`), []byte(`{"base":"TWD","notification":{"id":1}}`))
	if err != nil || sent != 4 || len(service.Received) != 4 {
		t.Fatalf("sent = %d, %v; received %d, want 4", sent, err, len(service.Received))
	}
	r := service.Received[0]
	if !strings.HasPrefix(r.Header.Get("Authorization"), "vapid t=") || r.Header.Get("Content-Encoding") != "aes128gcm" ||
		r.Header.Get("TTL") != "86400" || r.Header.Get("Urgency") != "high" {
		t.Fatalf("headers = %v", r.Header)
	}
}

func TestADeviceItsPushServiceSaysIsGoneIsDropped(t *testing.T) {
	pool := testdb.Pool(t)
	service := NewPushService(t)
	Device(t, pool, service, "/gone")
	Device(t, pool, service, "/here")
	service.Status["/gone"] = http.StatusGone

	if sent, err := push.Send(t.Context(), pool, []byte(`{}`)); err != nil || sent != 1 {
		t.Fatalf("sent = %d, %v; want 1", sent, err)
	}
	left, err := dbq.New(pool).PushSubscriptions(t.Context())
	if err != nil || len(left) != 1 || !strings.HasSuffix(left[0].Endpoint, "/here") {
		t.Fatalf("left = %+v, %v; want /here only", left, err)
	}

	// Any other failure is reported, and the device kept.
	service.Status["/here"] = http.StatusInternalServerError
	if _, err := push.Send(t.Context(), pool, []byte(`{}`)); err == nil || !strings.Contains(err.Error(), "answered 500") {
		t.Fatalf("err = %v, want the service's answer", err)
	}
}

func TestOnlyAPushServicesHTTPSEndpointIsTaken(t *testing.T) {
	queries := dbq.New(testdb.Pool(t))
	for _, endpoint := range []string{"http://push.example/1", "file:///etc/passwd", "/relative", ""} {
		if err := push.Subscribe(t.Context(), queries, endpoint, "k", "a"); !errors.Is(err, push.ErrNotHTTPS) {
			t.Errorf("%q: err = %v", endpoint, err)
		}
	}
}
