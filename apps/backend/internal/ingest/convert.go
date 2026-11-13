package ingest

import (
	"errors"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/hsin19/finchie/apps/backend/internal/db/dbq"
	"github.com/hsin19/finchie/apps/backend/internal/domain"
	parserv2 "github.com/hsin19/finchie/apps/backend/internal/gen/finchie/parser/v2"
)

// A statement reports accounts: their balances on a day and the lines between (ADR 0021). Converting one
// checks the contract, never the figures: whether they add up is the statement's checks'.

var accountKinds = map[parserv2.AccountKind]domain.AccountKind{
	parserv2.AccountKind_ACCOUNT_KIND_CREDIT_CARD: domain.CreditCard,
	parserv2.AccountKind_ACCOUNT_KIND_DEPOSIT:     domain.Deposit,
	parserv2.AccountKind_ACCOUNT_KIND_SECURITIES:  domain.Securities,
	parserv2.AccountKind_ACCOUNT_KIND_POINTS:      domain.Points,
	parserv2.AccountKind_ACCOUNT_KIND_LOAN:        domain.Loan,
}

var lineKinds = map[parserv2.LineKind]domain.LineKind{
	parserv2.LineKind_LINE_KIND_PURCHASE:                domain.Purchase,
	parserv2.LineKind_LINE_KIND_FOREIGN_TRANSACTION_FEE: domain.ForeignTransactionFee,
	parserv2.LineKind_LINE_KIND_ANNUAL_FEE:              domain.AnnualFee,
	parserv2.LineKind_LINE_KIND_REFUND:                  domain.Refund,
	parserv2.LineKind_LINE_KIND_REBATE:                  domain.Rebate,
	parserv2.LineKind_LINE_KIND_PAYMENT:                 domain.Payment,
	parserv2.LineKind_LINE_KIND_INTEREST:                domain.Interest,
	parserv2.LineKind_LINE_KIND_FEE:                     domain.Fee,
	parserv2.LineKind_LINE_KIND_OTHER:                   domain.Other,
	parserv2.LineKind_LINE_KIND_BUY:                     domain.Buy,
	parserv2.LineKind_LINE_KIND_SELL:                    domain.Sell,
	parserv2.LineKind_LINE_KIND_EARN:                    domain.Earn,
	parserv2.LineKind_LINE_KIND_ADJUST:                  domain.Adjust,
	parserv2.LineKind_LINE_KIND_REDEEM:                  domain.Redeem,
}

func convertResponse(documentID int64, response *parserv2.ParseDocumentResponse) (stored, error) {
	s := response.GetStatement()
	if s == nil {
		return stored{}, fmt.Errorf("%w: no statement", ErrInvalidResponse)
	}
	var errs []error
	statement := dbq.InsertStatementParams{DocumentID: documentID, Institution: s.GetInstitution(), Balanced: true}
	if statement.Institution == "" {
		errs = append(errs, errors.New("institution is missing"))
	}
	var err error
	if statement.PeriodStart, err = optionalDate(s.GetPeriodStart()); err != nil {
		errs = append(errs, fmt.Errorf("period start: %w", err))
	}
	statement.ClosingDate, errs = requiredDate(s.GetClosingDate(), "closing date", errs)

	bill := s.GetCardBill()
	if bill != nil {
		statement.Currency = optionalString(bill.GetCurrency())
		if statement.Currency == nil {
			errs = append(errs, errors.New("card bill: currency is missing"))
		}
		statement.DueDate, errs = requiredDate(bill.GetDueDate(), "card bill due date", errs)
		statement.TotalDue, errs = requiredNumeric(bill.GetTotalDue(), "card bill total due", errs)
	}

	result := stored{}
	var kinds []domain.AccountKind
	var numbers []string
	reported := map[[2]string]bool{}
	lineNo := int32(0)
	for i, a := range s.GetAccounts() {
		where := fmt.Sprintf("account %d", i)
		kind, ok := accountKinds[a.GetKind()]
		if !ok {
			errs = append(errs, fmt.Errorf("%s: kind %s", where, a.GetKind()))
			continue
		}
		if kind.Numbered() && a.GetNumber() == "" {
			errs = append(errs, fmt.Errorf("%s: a %s account needs its number", where, kind))
		}
		if key := [2]string{string(kind), a.GetNumber()}; reported[key] {
			errs = append(errs, fmt.Errorf("%s: %s %q reported twice", where, kind, a.GetNumber()))
		} else {
			reported[key] = true
		}
		kinds, numbers = append(kinds, kind), append(numbers, a.GetNumber())
		account := storedAccount{account: dbq.UpsertAccountParams{
			Institution: statement.Institution, Kind: kind, Number: a.GetNumber(), Label: optionalString(a.GetName()),
		}}

		for j, b := range a.GetBalances() {
			at := fmt.Sprintf("%s balance %d", where, j)
			if b.GetUnit() == "" {
				errs = append(errs, fmt.Errorf("%s: unit is missing", at))
			}
			balance := storedBalance{unit: b.GetUnit()}
			balance.AsOf, errs = requiredDate(b.GetAsOf(), at+" date", errs)
			balance.Amount, errs = requiredNumeric(b.GetAmount(), at+" amount", errs)
			account.balances = append(account.balances, balance)
		}
		// What a bill owes is its card account's balance on the closing date (ADR 0022).
		if kind == domain.CreditCard && bill != nil {
			account.balances = append(account.balances, storedBalance{unit: bill.GetCurrency(),
				InsertBalanceParams: dbq.InsertBalanceParams{AsOf: statement.ClosingDate, Amount: statement.TotalDue}})
		}

		for j, l := range a.GetLines() {
			at := fmt.Sprintf("%s line %d", where, j)
			line, trade, lineErrs := convertLine(at, kind, lineNo, l)
			errs = append(errs, lineErrs...)
			account.lines = append(account.lines, line)
			if trade != nil {
				result.trades.LineNos = append(result.trades.LineNos, lineNo)
				result.trades.Categories = append(result.trades.Categories, trade.category)
				result.trades.Prices, result.trades.Amounts = append(result.trades.Prices, trade.price), append(result.trades.Amounts, trade.amount)
				result.trades.Fees, result.trades.Taxes = append(result.trades.Fees, trade.fee), append(result.trades.Taxes, trade.tax)
				result.trades.Nets, result.trades.Prepaids = append(result.trades.Nets, trade.net), append(result.trades.Prepaids, trade.prepaid)
			}
			lineNo++
		}

		for j, e := range a.GetExpiries() {
			at := fmt.Sprintf("%s expiry %d", where, j)
			if e.GetUnit() == "" {
				errs = append(errs, fmt.Errorf("%s: unit is missing", at))
			}
			expiry := storedExpiry{unit: e.GetUnit()}
			expiry.Amount, errs = requiredNumeric(e.GetAmount(), at+" amount", errs)
			expiry.LapsesBy, errs = requiredDate(e.GetBy(), at+" date", errs)
			account.expiries = append(account.expiries, expiry)
		}
		for j, r := range a.GetLapseRules() {
			rule, err := lapseRule(r)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s lapse rule %d: %w", where, j, err))
				continue
			}
			account.lapseRules = append(account.lapseRules, storedLapseRule{unit: r.GetUnit(), rule: rule})
		}
		result.accounts = append(result.accounts, account)
	}
	switch {
	case len(s.GetAccounts()) == 0:
		errs = append(errs, errors.New("no accounts"))
	case bill != nil && !slices.Contains(kinds, domain.CreditCard):
		errs = append(errs, errors.New("a card bill reports no card account"))
	}

	for i, c := range s.GetChecks() {
		at := fmt.Sprintf("check %d", i)
		if c.GetName() == "" {
			errs = append(errs, fmt.Errorf("%s: name is missing", at))
		}
		var computed, stated pgtype.Numeric
		computed, errs = requiredNumeric(c.GetComputed(), at+" computed", errs)
		stated, errs = requiredNumeric(c.GetStated(), at+" stated", errs)
		checks := &result.checks
		checks.Names, checks.Units = append(checks.Names, c.GetName()), append(checks.Units, c.GetUnit())
		checks.Computed, checks.Stated = append(checks.Computed, computed), append(checks.Stated, stated)
		checks.Balanced = append(checks.Balanced, c.GetBalanced())
		statement.Balanced = statement.Balanced && c.GetBalanced()
	}

	if len(errs) > 0 {
		return stored{}, fmt.Errorf("%w: %w", ErrInvalidResponse, errors.Join(errs...))
	}
	statement.Kind = domain.StatementKind(bill != nil, kinds...)
	// A statement is the accounts it reports over its period (ADR 0013).
	statement.AccountNumbers = domain.AccountNumbers(numbers...)
	result.statement = statement
	return result, nil
}

// trade is a trade line's money (ADR 0017).
type trade struct {
	category                              string
	price, amount, fee, tax, net, prepaid pgtype.Numeric
}

func convertLine(at string, account domain.AccountKind, lineNo int32, l *parserv2.Line) (storedLine, *trade, []error) {
	var errs []error
	kind, ok := lineKinds[l.GetKind()]
	switch {
	case !ok:
		errs = append(errs, fmt.Errorf("%s: kind %s", at, l.GetKind()))
	case !slices.Contains(account.LineKinds(), kind):
		errs = append(errs, fmt.Errorf("%s: a %s account has no %s line", at, account, kind))
	}
	if l.GetUnit() == "" {
		errs = append(errs, fmt.Errorf("%s: unit is missing", at))
	}
	line := storedLine{unit: l.GetUnit(), InsertTransactionsParams: dbq.InsertTransactionsParams{
		LineNo: lineNo, Description: l.GetDescription(), Kind: kind,
	}}
	line.TransactionDate, errs = requiredDate(l.GetDate(), at+" date", errs)
	line.Amount, errs = requiredNumeric(l.GetAmount(), at+" amount", errs)

	var traded *trade
	switch d := l.GetDetail().(type) {
	case *parserv2.Line_Card:
		c := d.Card
		posting, err := optionalDate(c.GetPostingDate())
		if err != nil {
			errs = append(errs, fmt.Errorf("%s posting date: %w", at, err))
		}
		line.PostingDate = posting
		if line.OriginalAmount, err = numeric(c.GetOriginalAmount()); err != nil {
			errs = append(errs, fmt.Errorf("%s original amount: %w", at, err))
		}
		line.OriginalCurrency = optionalString(c.GetOriginalCurrency())
		if line.OriginalAmount.Valid != (line.OriginalCurrency != nil) {
			errs = append(errs, fmt.Errorf("%s: a foreign amount and its currency come together", at))
		}
		line.Country, line.CardLast4 = optionalString(c.GetCountry()), optionalString(c.GetCardLast4())
	case *parserv2.Line_Deposit:
		dep := d.Deposit
		line.Memo, line.Counterparty = optionalString(dep.GetMemo()), optionalString(dep.GetCounterparty())
		balance, err := numeric(dep.GetBalance())
		if err != nil {
			errs = append(errs, fmt.Errorf("%s balance: %w", at, err))
		}
		line.Balance = balance
	case *parserv2.Line_Trade:
		t := d.Trade
		traded = &trade{category: t.GetCategory()}
		traded.price, errs = requiredNumeric(t.GetPrice(), at+" price", errs)
		traded.amount, errs = requiredNumeric(t.GetAmount(), at+" trade amount", errs)
		traded.fee, errs = requiredNumeric(t.GetFee(), at+" fee", errs)
		traded.tax, errs = requiredNumeric(t.GetTax(), at+" tax", errs)
		traded.net, errs = requiredNumeric(t.GetNet(), at+" net", errs)
		prepaid, err := numeric(t.GetPrepaid())
		if err != nil {
			errs = append(errs, fmt.Errorf("%s prepaid: %w", at, err))
		}
		traded.prepaid = prepaid
	}
	if account == domain.Securities && traded == nil {
		errs = append(errs, fmt.Errorf("%s: a trade without its money", at))
	}
	return line, traded, errs
}

// lapseRule reads how a program's points lapse (ADR 0019): months after a bill's closing, or a yearly
// purge, each within its range.
func lapseRule(r *parserv2.LapseRule) (domain.LapseRule, error) {
	if r.GetUnit() == "" {
		return domain.LapseRule{}, errors.New("unit is missing")
	}
	switch rule := r.GetRule().(type) {
	case *parserv2.LapseRule_MonthsAfterClosing:
		if rule.MonthsAfterClosing <= 0 || rule.MonthsAfterClosing > 1200 {
			return domain.LapseRule{}, fmt.Errorf("%d months after closing", rule.MonthsAfterClosing)
		}
		return domain.LapseRule{MonthsAfterClosing: int(rule.MonthsAfterClosing)}, nil
	case *parserv2.LapseRule_YearlyPurge:
		last, years := rule.YearlyPurge.GetLastMonth(), rule.YearlyPurge.GetYears()
		if last < 1 || last > 12 || years <= 0 || years > 100 {
			return domain.LapseRule{}, fmt.Errorf("a yearly purge after month %d, %d years on", last, years)
		}
		return domain.LapseRule{PurgeLastMonth: int(last), PurgeYears: int(years)}, nil
	default:
		return domain.LapseRule{}, errors.New("no rule")
	}
}
