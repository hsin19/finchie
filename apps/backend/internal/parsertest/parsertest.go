// Package parsertest builds what the parser returns (contract v2, ADR 0021), for tests in any package.
// Every name and figure a test builds with it is made up (ADR 0006).
package parsertest

import (
	"google.golang.org/genproto/googleapis/type/date"
	"google.golang.org/genproto/googleapis/type/decimal"

	parserv2 "github.com/hsin19/finchie/apps/backend/internal/gen/finchie/parser/v2"
)

// Day is a date of 2026, the year every fixture is set in.
func Day(month, day int32) *date.Date { return &date.Date{Year: 2026, Month: month, Day: day} }

// Dec is a decimal as the parser prints it; nil for "".
func Dec(s string) *decimal.Decimal {
	if s == "" {
		return nil
	}
	return &decimal.Decimal{Value: s}
}

// Response is what a parser of this name returns for the statement, at version 1.
func Response(parser string, s *parserv2.Statement) *parserv2.ParseDocumentResponse {
	return &parserv2.ParseDocumentResponse{Parser: parser, ParserVersion: "1", Statement: s}
}

// Check is a figure the statement prints against what its lines come to.
func Check(name, unit, computed, stated string) *parserv2.Check {
	return &parserv2.Check{Name: name, Unit: unit, Computed: Dec(computed), Stated: Dec(stated), Balanced: computed == stated}
}

// Balance is what an account held of a unit at the end of a day.
func Balance(unit string, asOf *date.Date, amount string) *parserv2.Balance {
	return &parserv2.Balance{Unit: unit, AsOf: asOf, Amount: Dec(amount)}
}

// Bill is a card bill of the bank's, in NT$: its card lines, what it owes, and the check that its new
// charges add up.
func Bill(institution string, closing, due *date.Date, totalDue string, lines ...*parserv2.Line) *parserv2.Statement {
	return &parserv2.Statement{
		Institution: institution,
		ClosingDate: closing,
		CardBill:    &parserv2.CardBill{DueDate: due, Currency: "TWD", TotalDue: Dec(totalDue)},
		Accounts:    []*parserv2.AccountReport{{Kind: parserv2.AccountKind_ACCOUNT_KIND_CREDIT_CARD, Lines: lines}},
	}
}

// CardOption adds what a card line prints beside its amount.
type CardOption func(*parserv2.CardLine)

func Posted(on *date.Date) CardOption { return func(c *parserv2.CardLine) { c.PostingDate = on } }

func Foreign(currency, amount string) CardOption {
	return func(c *parserv2.CardLine) { c.OriginalCurrency, c.OriginalAmount = currency, Dec(amount) }
}

func Country(code string) CardOption { return func(c *parserv2.CardLine) { c.Country = code } }
func OnCard(last4 string) CardOption { return func(c *parserv2.CardLine) { c.CardLast4 = last4 } }

// CardLine is a line of a card bill in NT$, as printed: charges positive.
func CardLine(on *date.Date, description string, kind parserv2.LineKind, amount string, options ...CardOption) *parserv2.Line {
	card := &parserv2.CardLine{}
	for _, option := range options {
		option(card)
	}
	return &parserv2.Line{
		Date: on, Unit: "TWD", Amount: Dec(amount), Kind: kind, Description: description,
		Detail: &parserv2.Line_Card{Card: card},
	}
}

// Points adds the bank's points account to a bill.
func Points(s *parserv2.Statement, balances []*parserv2.Balance, lines []*parserv2.Line, expiries ...*parserv2.Expiry) *parserv2.Statement {
	s.Accounts = append(s.Accounts, &parserv2.AccountReport{
		Kind: parserv2.AccountKind_ACCOUNT_KIND_POINTS, Balances: balances, Lines: lines, Expiries: expiries,
	})
	return s
}

// PointsLine is a points movement on a bill's closing date.
func PointsLine(on *date.Date, program, label string, kind parserv2.LineKind, amount string) *parserv2.Line {
	return &parserv2.Line{Date: on, Unit: program, Amount: Dec(amount), Kind: kind, Description: label}
}

func Expiry(program, amount string, by *date.Date) *parserv2.Expiry {
	return &parserv2.Expiry{Unit: program, Amount: Dec(amount), By: by}
}

// Deposit is a deposit statement of the bank's over a period.
func Deposit(institution string, start, end *date.Date, accounts ...*parserv2.AccountReport) *parserv2.Statement {
	return &parserv2.Statement{Institution: institution, PeriodStart: start, ClosingDate: end, Accounts: accounts}
}

// DepositAccount is what a deposit statement reports of one account.
func DepositAccount(number, name string, balances []*parserv2.Balance, lines ...*parserv2.Line) *parserv2.AccountReport {
	return &parserv2.AccountReport{
		Kind: parserv2.AccountKind_ACCOUNT_KIND_DEPOSIT, Number: number, Name: name, Balances: balances, Lines: lines,
	}
}

// DepositOption adds what a deposit line prints beside its amount.
type DepositOption func(*parserv2.Line, *parserv2.DepositLine)

func Memo(memo string) DepositOption {
	return func(_ *parserv2.Line, d *parserv2.DepositLine) { d.Memo = memo }
}

func Counterparty(counterparty string) DepositOption {
	return func(_ *parserv2.Line, d *parserv2.DepositLine) { d.Counterparty = counterparty }
}

func Kind(kind parserv2.LineKind) DepositOption {
	return func(l *parserv2.Line, _ *parserv2.DepositLine) { l.Kind = kind }
}

// DepositLine is a line of a deposit account, money in positive, with the running balance after it when
// one is given.
func DepositLine(on *date.Date, description, unit, amount, balance string, options ...DepositOption) *parserv2.Line {
	deposit := &parserv2.DepositLine{Balance: Dec(balance)}
	line := &parserv2.Line{
		Date: on, Unit: unit, Amount: Dec(amount), Kind: parserv2.LineKind_LINE_KIND_OTHER, Description: description,
		Detail: &parserv2.Line_Deposit{Deposit: deposit},
	}
	for _, option := range options {
		option(line, deposit)
	}
	return line
}

// Securities is a brokerage account's statement: its holdings on the holdings date and its trades.
func Securities(institution, number string, start, holdings *date.Date, held []*parserv2.Balance, trades ...*parserv2.Line) *parserv2.Statement {
	return &parserv2.Statement{
		Institution: institution, PeriodStart: start, ClosingDate: holdings,
		Accounts: []*parserv2.AccountReport{{
			Kind: parserv2.AccountKind_ACCOUNT_KIND_SECURITIES, Number: number, Balances: held, Lines: trades,
		}},
	}
}

// Trade is a trade of a security: shares bought positive, sold negative, with its money.
func Trade(on *date.Date, security string, kind parserv2.LineKind, shares, price, amount, fee, tax, net string) *parserv2.Line {
	return &parserv2.Line{
		Date: on, Unit: security, Amount: Dec(shares), Kind: kind, Description: security,
		Detail: &parserv2.Line_Trade{Trade: &parserv2.Trade{
			Price: Dec(price), Category: "集買", Amount: Dec(amount), Fee: Dec(fee), Tax: Dec(tax), Net: Dec(net),
		}},
	}
}
