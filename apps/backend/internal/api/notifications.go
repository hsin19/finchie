package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v5"

	"github.com/hsin19/finchie/apps/backend/internal/db/dbq"
	"github.com/hsin19/finchie/apps/backend/internal/notify"
)

func (s *Server) ListNotifications(ctx context.Context, request ListNotificationsRequestObject) (ListNotificationsResponseObject, error) {
	queries := dbq.New(s.pool)
	open := request.Params.Open != nil && *request.Params.Open
	rows, err := queries.ListNotifications(ctx, dbq.ListNotificationsParams{Open: open, RowLimit: limit(request.Params.Limit)})
	if err != nil {
		return nil, fmt.Errorf("list notifications: %w", err)
	}
	unread, err := queries.CountUnreadNotifications(ctx)
	if err != nil {
		return nil, fmt.Errorf("count unread notifications: %w", err)
	}
	list := NotificationList{Items: make([]Notification, 0, len(rows)), Unread: int(unread)}
	for _, row := range rows {
		n, err := toNotification(row)
		if err != nil {
			return nil, err
		}
		list.Items = append(list.Items, n)
	}
	return ListNotifications200JSONResponse(list), nil
}

// toNotification shows a notification: its facts are stored as the API shows them (notify.Facts).
func toNotification(row dbq.ListNotificationsRow) (Notification, error) {
	var n Notification
	if err := json.Unmarshal(row.Facts, &n); err != nil {
		return n, fmt.Errorf("notification %d facts: %w", row.ID, err)
	}
	n.Id, n.Kind, n.RaisedAt = row.ID, NotificationKind(row.Kind), row.RaisedAt.Time
	n.ReadAt, n.HandledAt, n.ResolvedAt = optionalTime(row.ReadAt), optionalTime(row.HandledAt), optionalTime(row.ResolvedAt)
	if n.Document != nil {
		n.Document.Id = row.DocumentID
	}
	if row.AccountID != nil && row.Institution != nil {
		n.Account = &NotificationAccount{
			Id: *row.AccountID, Institution: *row.Institution, Kind: AccountKind(row.AccountKind), Label: row.Label, Name: row.Name,
		}
	}
	return n, nil
}

func optionalTime(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	return &t.Time
}

func (s *Server) ReadNotifications(ctx context.Context, request ReadNotificationsRequestObject) (ReadNotificationsResponseObject, error) {
	if err := dbq.New(s.pool).ReadNotifications(ctx, request.Body.Ids); err != nil {
		return nil, fmt.Errorf("read notifications: %w", err)
	}
	return ReadNotifications204Response{}, nil
}

func (s *Server) HandleNotifications(ctx context.Context, request HandleNotificationsRequestObject) (HandleNotificationsResponseObject, error) {
	err := dbq.New(s.pool).HandleNotifications(ctx, dbq.HandleNotificationsParams{Ids: request.Body.Ids, Handled: request.Body.Handled})
	if err != nil {
		return nil, fmt.Errorf("handle notifications: %w", err)
	}
	return HandleNotifications204Response{}, nil
}

// notifyAfterChanges works out notifications again once a request that may have changed the ledger is
// answered (notify), so that what the overview shows next is current. Uploads leave it to their parse
// job, and marking notifications changes nothing they are worked out from.
func notifyAfterChanges(pool *pgxpool.Pool) StrictMiddlewareFunc {
	skip := map[string]bool{
		"UploadDocument": true, "ReadNotifications": true, "HandleNotifications": true,
		"SubscribePush": true, "UnsubscribePush": true, "TestPush": true,
	}
	return func(next StrictHandlerFunc, operationID string) StrictHandlerFunc {
		return func(c *echo.Context, request any) (any, error) {
			response, err := next(c, request)
			if err == nil && c.Request().Method != http.MethodGet && !skip[operationID] {
				notify.After(c.Request().Context(), pool)
			}
			return response, err
		}
	}
}
