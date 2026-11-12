// Package demo holds small helpers shared by the examples. Nothing here talks
// to the network except FindRestingBuy and PickComboLegs, which read markets.
package demo

import (
	"fmt"
	"math/big"
	"regexp"
	"strings"
	"time"

	stx "github.com/stxapp/stx-go"
)

// BestLevel returns the best level on one side of a book: the highest bid
// (bid true) or the lowest offer (bid false). It picks by price rather than
// position, because a book can list its levels in either order. ok is false
// when the side is empty.
func BestLevel(levels []stx.BookLevel, bid bool) (best stx.BookLevel, ok bool) {
	for _, level := range levels {
		if !level.Price.Valid() {
			continue
		}
		if !ok {
			best, ok = level, true
			continue
		}
		c := level.Price.Cmp(best.Price)
		if (bid && c > 0) || (!bid && c < 0) {
			best = level
		}
	}
	return best, ok
}

// BestBid is the best bid price, or "-" when nobody is bidding.
func BestBid(m stx.Market) string {
	if l, ok := BestLevel(m.Bids, true); ok {
		return l.Price.String()
	}
	return "-"
}

// BestOffer is the best offer price, or "-" when nobody is offering.
func BestOffer(m stx.Market) string {
	if l, ok := BestLevel(m.Offers, false); ok {
		return l.Price.String()
	}
	return "-"
}

// IsUpcoming reports whether the market's event is still to come: scheduled
// (not started, finished or cancelled) and starting after now. An open
// market can belong to an event whose start has passed but whose status was
// never moved on, and nobody quotes those.
func IsUpcoming(m stx.Market, now time.Time) bool {
	if m.EventStatus != "" && m.EventStatus != "scheduled" {
		return false
	}
	return !m.EventStart.IsZero() && m.EventStart.After(now)
}

// DescribeMarket is one line: event, market title, best bid and offer.
func DescribeMarket(m stx.Market) string {
	return fmt.Sprintf("%s: %s  bid %s  offer %s", m.EventTitle, m.Title, BestBid(m), BestOffer(m))
}

// cents converts a dollar amount to whole cents, rounded to the nearest cent.
func cents(d stx.Decimal) (int64, bool) {
	r, ok := d.Rat()
	if !ok {
		return 0, false
	}
	if r.Sign() < 0 {
		return 0, false
	}
	// (num * 100 + denom / 2) / denom: whole cents, rounded half up.
	num := new(big.Int).Mul(r.Num(), big.NewInt(100))
	num.Add(num, new(big.Int).Quo(r.Denom(), big.NewInt(2)))
	c := num.Quo(num, r.Denom())
	if !c.IsInt64() {
		return 0, false
	}
	return c.Int64(), true
}

// fromCents formats whole cents as dollars with two decimals: 1 is "0.01".
func fromCents(c int64) stx.Decimal {
	return stx.Decimal(fmt.Sprintf("%d.%02d", c/100, c%100))
}

// RestingBuyPrice is a buy price far below anything a seller is likely to
// accept: 1% of what a winning contract pays, in whole cents, and never less
// than one cent. A market that pays "1.0000" gives "0.01"; one that pays
// "100.0000" gives "1.00". The price is read from the market, never assumed.
func RestingBuyPrice(maxPrice stx.Decimal) (stx.Decimal, error) {
	c, ok := cents(maxPrice)
	if !ok || c < 2 {
		return "", fmt.Errorf("cannot derive a resting price from max_price %q", maxPrice)
	}
	return fromCents(max(1, c/100)), nil
}

// WouldRest reports whether a buy at price would rest on the book instead of
// trading: nobody is offering, or the best offer is above price.
func WouldRest(m stx.Market, price stx.Decimal) bool {
	offer, ok := BestLevel(m.Offers, false)
	return !ok || offer.Price.Cmp(price) > 0
}

var stakePattern = regexp.MustCompile(`^\d+(\.\d{1,2})?$`)

// ComboStake turns "2" or "2.5" into a stake in whole cents, "2.00" or
// "2.50". The exchange takes whole cents and nothing below one cent.
func ComboStake(input string) (stx.Decimal, error) {
	s := strings.TrimSpace(input)
	if !stakePattern.MatchString(s) {
		return "", fmt.Errorf(`a stake is dollars in whole cents, e.g. "2" or "2.50", not %q`, input)
	}
	c, _ := cents(stx.Decimal(s))
	if c < 1 {
		return "", fmt.Errorf("a stake must be at least one cent, not %q", input)
	}
	return fromCents(c), nil
}
