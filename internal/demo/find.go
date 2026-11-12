package demo

import (
	"context"
	"errors"
	"time"

	stx "github.com/stxapp/stx-go"
)

// openMarkets is the query the examples scan: open markets accepting orders,
// in one sport when sport is set (e.g. "Football").
func openMarkets(sport string) *stx.MarketsQuery {
	q := &stx.MarketsQuery{Status: []string{"open"}, Trading: stx.Bool(true)}
	if sport != "" {
		q.Sports = []string{sport}
	}
	return q
}

// FindRestingBuy returns the first open market that is accepting orders,
// whose event is upcoming (see IsUpcoming), and where a buy at
// RestingBuyPrice would rest, with that price. ok is false when none of the
// first scan markets fit.
func FindRestingBuy(ctx context.Context, client *stx.Client, sport string, scan int, now time.Time) (market stx.Market, price stx.Decimal, ok bool, err error) {
	seen := 0
	for m, err := range client.Markets.All(ctx, openMarkets(sport)) {
		if err != nil {
			return stx.Market{}, "", false, err
		}
		if seen++; seen > scan {
			break
		}
		if m.MarketID == "" || m.MaxPrice == "" || !IsUpcoming(m, now) {
			continue
		}
		p, err := RestingBuyPrice(m.MaxPrice)
		if err != nil {
			continue
		}
		if WouldRest(m, p) {
			return m, p, true, nil
		}
	}
	return stx.Market{}, "", false, nil
}

// PickComboLegs returns up to count legs for a combo: open markets accepting
// orders, whose event is upcoming, each from a different event (two markets
// of one game can contradict each other), and never a combo itself. Fewer
// than count come back when the first scan markets do not have enough.
func PickComboLegs(ctx context.Context, client *stx.Client, count int, sport string, scan int, now time.Time) ([]stx.Market, error) {
	var legs []stx.Market
	events := map[string]bool{}
	seen := 0
	for m, err := range client.Markets.All(ctx, openMarkets(sport)) {
		if err != nil {
			return nil, err
		}
		if seen++; seen > scan || len(legs) >= count {
			break
		}
		if m.MarketID == "" || m.Archived || m.EventType == "combo" || !IsUpcoming(m, now) {
			continue
		}
		event := m.EventID
		if event == "" {
			event = m.MarketID
		}
		if events[event] {
			continue
		}
		events[event] = true
		legs = append(legs, m)
	}
	return legs, nil
}

// ReadOrder reads an order until done reports true, for up to wait. Order
// reads can trail a placement or a cancel by a few seconds on some
// exchanges, so a read straight after one can miss the order (not found)
// or show its previous status. It returns the last order read.
func ReadOrder(ctx context.Context, client *stx.Client, orderID string, wait time.Duration, done func(*stx.Order) bool) (*stx.Order, error) {
	deadline := time.Now().Add(wait)
	for {
		order, err := client.Orders.Get(ctx, orderID)
		if err == nil && done(order) {
			return order, nil
		}
		if err != nil && !errors.Is(err, stx.ErrNotFound) {
			return nil, err
		}
		if time.Now().After(deadline) {
			return order, err
		}
		select {
		case <-ctx.Done():
			return order, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}
