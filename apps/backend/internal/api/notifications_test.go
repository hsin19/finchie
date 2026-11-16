package api

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/hsin19/finchie/apps/backend/internal/notify"
	pt "github.com/hsin19/finchie/apps/backend/internal/parsertest"
	"github.com/hsin19/finchie/apps/backend/internal/taipei"
)

func shortfallsIn(list NotificationList) []Notification {
	var found []Notification
	for _, n := range list.Items {
		if n.Kind == NotificationKindShortfall {
			found = append(found, n)
		}
	}
	return found
}

func TestNotificationsAreListedReadHandledAndWorkedOutAgainAfterAChange(t *testing.T) {
	e, pool := newAPI(t, 1<<20)
	// E.SUN's savings paid September's bill and held 5,000 at 9/30; October's bill of 9,000 is due 10/22.
	store(t, pool, "esun.eml", septemberDeposit("esun", savings, "5000", septemberLine(22, "-6350", "玉山卡款扣繳", "")))
	store(t, pool, "bill.eml", septemberBill("6350"))
	store(t, pool, "october.eml", pt.Response("esun.credit_card", pt.Bill("esun", pt.Day(10, 7), pt.Day(10, 22), "9000")))
	if err := notify.Evaluate(t.Context(), pool, taipei.At(time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC), 9, 0)); err != nil {
		t.Fatal(err)
	}

	list := decode[NotificationList](t, get(t, e, "/api/notifications?open=true"))
	found := shortfallsIn(list)
	if len(found) != 1 || list.Unread != len(list.Items) {
		t.Fatalf("list = %+v, want one shortfall, every one unread", list)
	}
	n := found[0]
	esun := accountNamed(t, decode[[]Account](t, get(t, e, "/api/accounts")), savings)
	if n.Account == nil || n.Account.Id != esun.Id || n.Account.Institution != "esun" || n.Shortfall == nil ||
		n.Shortfall.Lacks != "4000" || n.Shortfall.AsOf.String() != "2026-09-30" || n.Shortfall.Debits[0].DueDate.String() != "2026-10-22" {
		t.Fatalf("notification = %+v, want E.SUN's savings 4,000 short of October's bill", n)
	}

	ids := fmt.Sprintf(`{"ids": [%d]}`, n.Id)
	if got := postJSON(t, e, "/api/notifications/read", ids); got.Code != http.StatusNoContent {
		t.Fatalf("read: %d %s", got.Code, got.Body)
	}
	if after := decode[NotificationList](t, get(t, e, "/api/notifications?open=true")); after.Unread != list.Unread-1 {
		t.Fatalf("unread after reading = %d, want %d", after.Unread, list.Unread-1)
	}
	if got := postJSON(t, e, "/api/notifications/handle", fmt.Sprintf(`{"ids": [%d], "handled": true}`, n.Id)); got.Code != http.StatusNoContent {
		t.Fatalf("handle: %d %s", got.Code, got.Body)
	}
	if handled := shortfallsIn(decode[NotificationList](t, get(t, e, "/api/notifications?open=true"))); len(handled) != 1 || handled[0].HandledAt == nil {
		t.Fatalf("handled = %+v", handled)
	}

	// A change the owner makes works them out again, today: October's bill is long due, so nothing is
	// still to come out of the account.
	if got := patch(t, e, fmt.Sprintf("/api/accounts/%d", esun.Id), `{"name": "薪轉"}`); got.Code != http.StatusNoContent {
		t.Fatalf("patch: %d", got.Code)
	}
	if open := shortfallsIn(decode[NotificationList](t, get(t, e, "/api/notifications?open=true"))); len(open) != 0 {
		t.Fatalf("open after the change = %+v, want none", open)
	}
	all := shortfallsIn(decode[NotificationList](t, get(t, e, "/api/notifications")))
	if len(all) != 1 || all[0].ResolvedAt == nil || all[0].Account.Name == nil || *all[0].Account.Name != "薪轉" {
		t.Fatalf("all = %+v, want it resolved, under the account's new name", all)
	}
}
