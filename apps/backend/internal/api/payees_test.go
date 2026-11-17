package api

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"
)

// Synthetic figures (ADR 0006).
const landlord = "00442168037069"

func TestALineIsFoundByItsCounterpartyAndByTheOwnersNameForIt(t *testing.T) {
	e, pool := newAPI(t, 1<<20)
	store(t, pool, "esun.eml", septemberDeposit("esun", savings, "45000",
		septemberLine(1, "-15000", "行動自轉", landlord),
		septemberLine(5, "60000", "薪資", ""),
	))
	search := func(q string) TransactionSearch {
		t.Helper()
		return decode[TransactionSearch](t, get(t, e, "/api/transactions?q="+url.QueryEscape(q)))
	}

	for _, q := range []string{landlord, "037069"} {
		if found := search(q); found.Total != 1 || found.Items[0].Description != "行動自轉" || found.Items[0].CounterpartyName != nil {
			t.Fatalf("%s: %+v, want the transfer, unnamed", q, found.Items)
		}
	}

	if got := postJSON(t, e, "/api/payees", fmt.Sprintf(`{"counterparty": %q, "name": " 房東 "}`, landlord)); got.Code != http.StatusNoContent {
		t.Fatalf("name: %d %s", got.Code, got.Body)
	}
	named := search("房東")
	if named.Total != 1 || named.Items[0].CounterpartyName == nil || *named.Items[0].CounterpartyName != "房東" {
		t.Fatalf("by name = %+v", named.Items)
	}
	esun := accountNamed(t, decode[[]Account](t, get(t, e, "/api/accounts")), savings)
	lines := decode[[]AccountLine](t, get(t, e, fmt.Sprintf("/api/accounts/%d/lines", esun.Id)))
	var shown *string
	for _, l := range lines {
		if l.Counterparty != nil && *l.Counterparty == landlord {
			shown = l.CounterpartyName
		}
	}
	if shown == nil || *shown != "房東" {
		t.Fatalf("the account's line shows %v, want 房東", shown)
	}

	// An empty name forgets it.
	if got := postJSON(t, e, "/api/payees", fmt.Sprintf(`{"counterparty": %q, "name": ""}`, landlord)); got.Code != http.StatusNoContent {
		t.Fatalf("forget: %d", got.Code)
	}
	if found := search("房東"); found.Total != 0 {
		t.Fatalf("after forgetting = %+v", found.Items)
	}
	if got := postJSON(t, e, "/api/payees", `{"counterparty": "", "name": "誰"}`); got.Code != http.StatusBadRequest {
		t.Fatalf("without a counterparty: %d", got.Code)
	}
}
