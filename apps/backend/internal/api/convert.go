package api

import (
	"cmp"
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
		LineCount:     int(row.LineCount),
		AccountCount:  int(row.AccountCount),
	}
	if row.StatementKind != nil {
		kind := AccountKind(*row.StatementKind)
		doc.StatementKind = &kind
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
		PaidOn:           optionalDate(row.PaidOn),
	}
}

func optionalDate(d pgtype.Date) *openapi_types.Date {
	if !d.Valid {
		return nil
	}
	return &openapi_types.Date{Time: d.Time}
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
		AccountName:      row.AccountName,
		Unit:             &row.Unit,
		Memo:             row.Memo,
		Counterparty:     row.Counterparty,
		Counterpart: lineCounterpart(row.CounterpartID, row.CounterpartStatementID, row.CounterpartDate, row.CounterpartAccountID,
			row.CounterpartAccountKind, row.CounterpartInstitution, row.CounterpartAccountName,
			cmp.Or(row.CounterpartFullNumber, row.CounterpartAccountNumber)),
		Trade: tradeOf(row.TradeCategory, row.TradePrice, row.TradeAmount, row.TradeFee, row.TradeTax, row.TradeNet, row.TradePrepaid),
	}
	accountKind := AccountKind(row.AccountKind)
	tx.AccountKind = &accountKind
	// A card bill's container is no account the owner has (ADR 0013): a card line names its card instead.
	if accountKind != CreditCard {
		tx.AccountId, tx.AccountNumber = &row.AccountID, &row.AccountNumber
	}
	if row.Balance.Valid {
		balance := decimalString(row.Balance)
		tx.Balance = &balance
	}
	if row.Flow.Valid {
		flow := decimalString(row.Flow)
		tx.Flow = &flow
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

// tradeOf is a trade's money from the trade columns a line query selects (ADR 0017); nil for any other line.
func tradeOf(category *string, price, amount, fee, tax, net, prepaid pgtype.Numeric) *Trade {
	if category == nil {
		return nil
	}
	trade := Trade{
		Category: *category, Price: decimalString(price), Amount: decimalString(amount), Fee: decimalString(fee),
		Tax: decimalString(tax), Net: decimalString(net),
	}
	if prepaid.Valid {
		value := decimalString(prepaid)
		trade.Prepaid = &value
	}
	return &trade
}

// lineCounterpart is the other line of a link (ADR 0016), from the counterpart columns a line query
// selects; nil when the line has none.
func lineCounterpart(id, statementID *int64, date pgtype.Date, accountID *int64, kind, institution, name, number *string) *LineCounterpart {
	if id == nil {
		return nil
	}
	counterpart := LineCounterpart{
		Id: *id, StatementId: *statementID, Date: openapi_types.Date{Time: date.Time},
		AccountKind: AccountKind(*kind), Institution: *institution, AccountName: name,
	}
	// A card bill's container is no account to point at (ADR 0013).
	if counterpart.AccountKind != CreditCard {
		counterpart.AccountId, counterpart.AccountNumber = accountID, number
	}
	return &counterpart
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
