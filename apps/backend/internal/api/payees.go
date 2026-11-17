package api

import (
	"context"
	"fmt"
	"strings"

	"github.com/hsin19/finchie/apps/backend/internal/db/dbq"
)

// NamePayee keeps the owner's name for a counterparty, or forgets it: a name only shows, and finds, the
// lines of that counterparty.
func (s *Server) NamePayee(ctx context.Context, request NamePayeeRequestObject) (NamePayeeResponseObject, error) {
	counterparty, name := request.Body.Counterparty, strings.TrimSpace(request.Body.Name)
	if counterparty == "" {
		return NamePayee400JSONResponse{ProblemJSONResponse{Message: "a counterparty to name is required"}}, nil
	}
	queries := dbq.New(s.pool)
	if name == "" {
		if err := queries.ForgetPayee(ctx, counterparty); err != nil {
			return nil, fmt.Errorf("forget payee: %w", err)
		}
		return NamePayee204Response{}, nil
	}
	if err := queries.NamePayee(ctx, dbq.NamePayeeParams{Counterparty: counterparty, Name: name}); err != nil {
		return nil, fmt.Errorf("name payee: %w", err)
	}
	return NamePayee204Response{}, nil
}
