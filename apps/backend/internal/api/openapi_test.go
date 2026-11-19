package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v5"

	contract "github.com/hsin19/finchie/apps/backend/api"
)

func TestTheContractIsServedAsJSON(t *testing.T) {
	e := echo.New()
	Register(e, NewServer(nil, nil, nil, 0, ""))

	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/openapi.json", nil)
	recorder := httptest.NewRecorder()
	e.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", recorder.Code, recorder.Body)
	}
	if got := recorder.Header().Get(echo.HeaderContentType); got != echo.MIMEApplicationJSON {
		t.Fatalf("content type = %q", got)
	}
	want, err := contract.JSON()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(recorder.Body.Bytes(), want) {
		t.Fatal("body is not the contract as converted")
	}
}
