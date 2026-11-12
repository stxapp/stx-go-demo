package demo

import (
	"fmt"
	"math/big"
	"regexp"
	"strings"

	stx "github.com/stxapp/stx-go"
)

// Amounts and ticks as integers of 10^-8 dollars, so no price is rounded
// through a float.
var scale = big.NewInt(100_000_000)

var unitsPattern = regexp.MustCompile(`^(\d+)(?:\.(\d+))?$`)

func units(d stx.Decimal) (*big.Int, error) {
	m := unitsPattern.FindStringSubmatch(strings.TrimSpace(string(d)))
	if m == nil {
		return nil, fmt.Errorf("not a decimal string: %q", d)
	}
	frac := (m[2] + "00000000")[:8]
	whole, _ := new(big.Int).SetString(m[1], 10)
	f, _ := new(big.Int).SetString(frac, 10)
	return whole.Mul(whole, scale).Add(whole, f), nil
}

// centsString formats units, rounded down to whole cents, as dollars: 333250000 is "3.33".
func centsString(u *big.Int) stx.Decimal {
	c := new(big.Int).Quo(u, big.NewInt(1_000_000))
	q, r := new(big.Int).QuoRem(c, big.NewInt(100), new(big.Int))
	return stx.Decimal(fmt.Sprintf("%s.%02d", q, r.Int64()))
}

// QuoteRequest is what MakerQuoteAmount needs of a combo request.
type QuoteRequest struct {
	// Side is "buy" (a player buying a combo) or "sell" (cashing one out).
	Side string
	// Amount is the stake, on a buy.
	Amount stx.Decimal
	// Quantity is the number of contracts sold, on a cash out.
	Quantity stx.Decimal
	// MaxPrice is what a winning contract pays.
	MaxPrice stx.Decimal
	// TickSize is the price increment of the combo's market.
	TickSize stx.Decimal
}

// MakerQuoteAmount is the amount a maker quotes on a request, in whole cents,
// or ok false when the exchange would refuse it.
//
// On a buy the amount is what the player wins: the stake times winMultiple.
// On a cash out it is what the maker pays for the whole size: cashOutPrice a
// contract. The exchange derives a price from the amount, rounded to the
// request's tick size in the maker's favor, and refuses a quote whose price
// is not above 0 and below the max price (price_out_of_range), or, on a buy,
// whose stake buys no whole contract (below_one_contract). This checks the
// same rules first, so the example does not send a quote it knows will fail.
func MakerQuoteAmount(req QuoteRequest, winMultiple, cashOutPrice stx.Decimal) (amount stx.Decimal, ok bool, err error) {
	maxPrice, err := units(req.MaxPrice)
	if err != nil {
		return "", false, err
	}
	tick, err := units(req.TickSize)
	if err != nil {
		return "", false, err
	}
	if tick.Sign() == 0 {
		return "", false, fmt.Errorf("tick size is zero")
	}
	cent := big.NewInt(1_000_000)
	toCents := func(u *big.Int) *big.Int { return new(big.Int).Mul(new(big.Int).Quo(u, cent), cent) }

	if req.Side == "buy" {
		stake, err := units(orZero(req.Amount))
		if err != nil {
			return "", false, err
		}
		mult, err := units(winMultiple)
		if err != nil {
			return "", false, err
		}
		amt := toCents(new(big.Int).Quo(new(big.Int).Mul(stake, mult), scale))
		if amt.Sign() <= 0 {
			return "", false, nil
		}
		// max_price x stake / (stake + amount), rounded up to the tick.
		num := new(big.Int).Mul(maxPrice, stake)
		den := new(big.Int).Mul(new(big.Int).Add(stake, amt), tick)
		steps := new(big.Int).Quo(new(big.Int).Add(num, new(big.Int).Sub(den, big.NewInt(1))), den)
		price := steps.Mul(steps, tick)
		if price.Sign() <= 0 || price.Cmp(maxPrice) >= 0 || new(big.Int).Quo(stake, price).Sign() == 0 {
			return "", false, nil
		}
		return centsString(amt), true, nil
	}

	qty, err := units(orZero(req.Quantity))
	if err != nil {
		return "", false, err
	}
	per, err := units(cashOutPrice)
	if err != nil {
		return "", false, err
	}
	if qty.Sign() <= 0 {
		return "", false, nil
	}
	amt := toCents(new(big.Int).Quo(new(big.Int).Mul(qty, per), scale))
	// amount / quantity, rounded down to the tick.
	steps := new(big.Int).Quo(new(big.Int).Mul(amt, scale), new(big.Int).Mul(qty, tick))
	price := steps.Mul(steps, tick)
	if price.Sign() <= 0 || price.Cmp(maxPrice) >= 0 {
		return "", false, nil
	}
	return centsString(amt), true, nil
}

func orZero(d stx.Decimal) stx.Decimal {
	if d == "" {
		return "0"
	}
	return d
}
