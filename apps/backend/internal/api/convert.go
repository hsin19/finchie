package api

import (
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/hsin19/finchie/apps/backend/internal/db/dbq"
)

func toDocument(row dbq.GetDocumentRow) Document {
	doc := Document{
		Id:            row.ID,
		Filename:      row.Filename,
		ContentType:   row.ContentType,
		SizeBytes:     int(row.SizeBytes),
		Sha256:        hex.EncodeToString(row.Sha256),
		ReceivedAt:    row.ReceivedAt.Time,
		Status:        DocumentStatus(row.Status),
		Error:         row.Error,
		Parser:        row.Parser,
		ParserVersion: row.ParserVersion,
		Warnings:      row.Warnings,
	}
	if doc.Warnings == nil {
		doc.Warnings = []string{}
	}
	if row.ParsedAt.Valid {
		doc.ParsedAt = &row.ParsedAt.Time
	}
	return doc
}

// toStatement takes a card bill: the statement endpoints list no other kind yet, and a bill's columns
// are never null (statements_card_bill_check).
func toStatement(row dbq.ListStatementsRow) Statement {
	return Statement{
		Id:          row.ID,
		DocumentId:  row.DocumentID,
		Institution: row.Institution,
		ClosingDate: openapi_types.Date{Time: row.ClosingDate.Time},
		DueDate:     openapi_types.Date{Time: row.DueDate.Time},
		Currency:    deref(row.Currency),
		TotalDue:    decimalString(row.TotalDue),
		Reconciliation: Reconciliation{
			Balanced:    row.Balanced,
			Computed:    decimalString(row.ComputedNewCharges),
			Stated:      decimalString(row.StatedNewCharges),
			StatedLabel: deref(row.StatedLabel),
		},
		TransactionCount: int(row.TransactionCount),
	}
}

func toTransaction(row dbq.ListTransactionsRow) (Transaction, error) {
	tx := Transaction{
		Id:               row.ID,
		StatementId:      row.StatementID,
		Institution:      row.Institution,
		TransactionDate:  openapi_types.Date{Time: row.TransactionDate.Time},
		Description:      row.Description,
		Kind:             TransactionKind(row.Kind),
		Amount:           decimalString(row.Amount),
		OriginalCurrency: row.OriginalCurrency,
		Country:          row.Country,
		CardLast4:        row.CardLast4,
	}
	if row.PostingDate.Valid {
		tx.PostingDate = &openapi_types.Date{Time: row.PostingDate.Time}
	}
	if row.OriginalAmount.Valid {
		original := decimalString(row.OriginalAmount)
		tx.OriginalAmount = &original
	}
	if row.Parent != nil {
		var parent TransactionLink
		if err := json.Unmarshal(row.Parent, &parent); err != nil {
			return Transaction{}, fmt.Errorf("parent of transaction %d: %w", row.ID, err)
		}
		tx.Parent = &parent
	}
	if row.Children != nil {
		var children []TransactionLink
		if err := json.Unmarshal(row.Children, &children); err != nil {
			return Transaction{}, fmt.Errorf("children of transaction %d: %w", row.ID, err)
		}
		tx.Children = &children
	}
	return tx, nil
}

// decimalString renders NUMERIC exactly as Postgres stores it ("780.00", "-6553").
func decimalString(n pgtype.Numeric) string {
	value, err := n.Value()
	if err != nil || value == nil {
		return ""
	}
	s, _ := value.(string)
	return s
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
