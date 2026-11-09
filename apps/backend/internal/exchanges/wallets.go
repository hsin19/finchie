package exchanges

import (
	"context"
	"log/slog"
	"math/big"
	"strings"
)

// wallet is one of an exchange's wallets (spot, funding, earn, futures, …): the coins it lists, if it lists
// any, and the value the exchange gives it in USDT.
type wallet struct {
	name  string
	coins map[string]*big.Rat
	value *big.Rat
}

// fold turns an exchange's wallets into a snapshot whose valuation is their values added up (ADR 0018).
// A wallet is listed by coin when its coins, at the exchange's own prices, come to its value; otherwise it
// goes in by its value, in USDT. The exchanges leave much undocumented (which fields a coin's total takes,
// whether margin is net of what is borrowed, whether an earn product also shows in spot), and a wallet
// whose coins do not add up to its value is one read wrong: by its value, the account's worth stays right.
func fold(ctx context.Context, institution string, wallets []wallet, prices map[string]*big.Rat) Snapshot {
	var snapshot Snapshot
	total := new(big.Rat)
	for _, w := range wallets {
		total.Add(total, w.value)
		if w.coins != nil {
			priced := pricedAt(w.coins, prices)
			if agrees(priced, w.value) {
				for coin, amount := range w.coins {
					if amount.Sign() != 0 {
						snapshot.add(coin, amount)
					}
				}
				continue
			}
			slog.InfoContext(ctx, "exchange wallet listed by its value: its coins do not add up to it",
				"institution", institution, "wallet", w.name, "coins_usdt", priced.FloatString(2), "value_usdt", w.value.FloatString(2))
		}
		if w.value.Sign() != 0 {
			snapshot.add("USDT", w.value)
		}
	}
	snapshot.Valuation = &Value{Unit: "USDT", Amount: total}
	return snapshot
}

// pricedAt is what coins come to in USDT. A coin without a price counts for nothing: dust of a coin that
// no market lists any more.
func pricedAt(coins map[string]*big.Rat, prices map[string]*big.Rat) *big.Rat {
	priced := new(big.Rat)
	for coin, amount := range coins {
		if price, ok := prices[strings.ToUpper(coin)]; ok {
			priced.Add(priced, new(big.Rat).Mul(amount, price))
		}
	}
	return priced
}

// agrees reports whether coins priced at a wallet's value come to it: within 1%, or 1 USDT for a small
// wallet, since the prices and the value are read moments apart.
func agrees(priced, value *big.Rat) bool {
	diff := new(big.Rat).Sub(priced, value)
	diff.Abs(diff)
	tolerance := new(big.Rat).Quo(new(big.Rat).Abs(value), big.NewRat(100, 1))
	if tolerance.Cmp(big.NewRat(1, 1)) < 0 {
		tolerance = big.NewRat(1, 1)
	}
	return diff.Cmp(tolerance) <= 0
}

// usdtPrices are coins' prices in USDT from an exchange's spot markets, by symbol (BTCUSDT): a coin's
// USDT market, else its BTC market at BTC's.
func usdtPrices(markets map[string]*big.Rat) map[string]*big.Rat {
	prices := map[string]*big.Rat{"USDT": big.NewRat(1, 1)}
	for symbol, price := range markets {
		if coin, ok := strings.CutSuffix(symbol, "USDT"); ok && coin != "" && price.Sign() > 0 {
			prices[coin] = price
		}
	}
	btc, ok := prices["BTC"]
	if !ok {
		return prices
	}
	for symbol, price := range markets {
		coin, ok := strings.CutSuffix(symbol, "BTC")
		if _, priced := prices[coin]; ok && coin != "" && !priced && price.Sign() > 0 {
			prices[coin] = new(big.Rat).Mul(price, btc)
		}
	}
	return prices
}
