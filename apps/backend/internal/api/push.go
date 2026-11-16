package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/hsin19/finchie/apps/backend/internal/db/dbq"
	"github.com/hsin19/finchie/apps/backend/internal/domain"
	"github.com/hsin19/finchie/apps/backend/internal/notify"
	"github.com/hsin19/finchie/apps/backend/internal/push"
)

func (s *Server) SubscribePush(ctx context.Context, request SubscribePushRequestObject) (SubscribePushResponseObject, error) {
	b := request.Body
	err := push.Subscribe(ctx, dbq.New(s.pool), b.Endpoint, b.Keys.P256dh, b.Keys.Auth)
	if errors.Is(err, push.ErrNotHTTPS) {
		return SubscribePush400JSONResponse{ProblemJSONResponse{Message: err.Error()}}, nil
	}
	if err != nil {
		return nil, err
	}
	return SubscribePush204Response{}, nil
}

func (s *Server) UnsubscribePush(ctx context.Context, request UnsubscribePushRequestObject) (UnsubscribePushResponseObject, error) {
	if err := dbq.New(s.pool).DeletePushSubscription(ctx, request.Body.Endpoint); err != nil {
		return nil, fmt.Errorf("drop push subscription: %w", err)
	}
	return UnsubscribePush204Response{}, nil
}

// TestPush pushes a message without a notification, which the app's service worker shows as a test.
func (s *Server) TestPush(ctx context.Context, _ TestPushRequestObject) (TestPushResponseObject, error) {
	message, err := json.Marshal(notify.Message{Base: domain.BaseCurrency})
	if err != nil {
		return nil, fmt.Errorf("test push: %w", err)
	}
	sent, err := push.Send(ctx, s.pool, message)
	if err != nil && sent == 0 {
		return nil, fmt.Errorf("test push: %w", err)
	}
	return TestPush200JSONResponse{Sent: sent}, nil
}
