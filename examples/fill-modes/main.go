// Fill-modes places post-only and fill-or-kill orders and shows what the
// exchange does with each:
//
//  1. A post-only buy at a price nobody offers: it rests on the book, then
//     the program cancels it.
//  2. A post-only buy at the best offer of another market: it would trade on
//     arrival, so the exchange cancels it (cancellation reason auto_matching)
//     and nothing trades.
//  3. A fill-or-kill buy at a price nobody offers: it cannot fill in full,
//     so the exchange cancels it with nothing filled.
//
// An exchange that has not enabled a mode refuses the order, and the program
// says so. An exchange without fill modes may place the order as a normal
// limit order instead: the program spots that from the placed order's
// FillMode, cancels it, and skips the steps that would trade. It needs a
// read_write API key.
//
//	go run ./examples/fill-modes
//	SPORT=Football go run ./examples/fill-modes   (only look at markets in that sport)
package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	stx "github.com/stxapp/stx-go"
	"github.com/stxapp/stx-go-demo/internal/demo"
)

func main() {
	log.SetFlags(0)
	client, err := stx.New()
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()

	market, price, ok, err := demo.FindRestingBuy(ctx, client, os.Getenv("SPORT"), 1000, time.Now())
	if err != nil {
		log.Fatal(err)
	}
	if !ok {
		fmt.Println("No open market right now where a small buy would rest. Try again later.")
		return
	}
	fmt.Printf("Market:  %s: %s\n", market.EventTitle, market.Title)

	// 1. Post-only at a price nobody offers: it rests as a maker.
	fmt.Printf("\n1. Post-only buy 1 @ %s\n", price)
	order, err := place(ctx, client, market.MarketID, price, stx.PostOnly)
	if done := explain(err); done {
		return
	}
	if order.FillMode != stx.PostOnly {
		// This exchange has no fill modes and placed a normal order. A
		// post-only or fill-or-kill order there would trade like any other
		// order, so stop before the steps that cross the book.
		fmt.Printf("Placed as a normal order (fill mode %q): this exchange has no fill modes.\n", order.FillMode)
		cancel(ctx, client, order.ID)
		fmt.Println("Skipping the post-only and fill-or-kill orders that would trade.")
		return
	}
	resting, err := demo.ReadOrder(ctx, client, order.ID, time.Minute, func(o *stx.Order) bool { return o.Status == "open" })
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Resting: status %s  fill mode %s  filled %s of %s\n", resting.Status, resting.FillMode, resting.Filled, resting.Quantity)
	cancel(ctx, client, order.ID)

	// 2. Post-only at the best offer: it would take that offer, so the
	//    exchange cancels it instead.
	crossMarket, offer, ok, err := findOffer(ctx, client, os.Getenv("SPORT"))
	if err != nil {
		log.Fatal(err)
	}
	if !ok {
		fmt.Println("\n2. No market with an offer right now; skipping the post-only order that would cross.")
	} else {
		fmt.Printf("\n2. Post-only buy 1 @ %s, the best offer on %s\n", offer, crossMarket.Symbol)
		order, err = place(ctx, client, crossMarket.MarketID, offer, stx.PostOnly)
		var wouldMatch *stx.PostOnlyWouldMatchError
		switch {
		case errors.As(err, &wouldMatch) && wouldMatch.Order != nil:
			o := wouldMatch.Order
			fmt.Printf("Cancelled: status %s  cancellation reason %s  filled %s\n", o.Status, o.CancellationReason, filled(o.Filled))
		case errors.As(err, &wouldMatch):
			// The best offer is your own order.
			fmt.Printf("Not placed: it would match your own order (%s)\n", wouldMatch.Reason)
		case err != nil:
			explain(err)
		default:
			// The exchange may accept it first and cancel it a moment later.
			o, rerr := demo.ReadOrder(ctx, client, order.ID, time.Minute, func(o *stx.Order) bool { return o.Status != "accepted" && o.Status != "open" })
			if rerr != nil {
				log.Fatal(rerr)
			}
			fmt.Printf("Cancelled: status %s  cancellation reason %s  filled %s\n", o.Status, o.CancellationReason, filled(o.Filled))
		}
	}

	// 3. Fill-or-kill at a price nobody offers: it cannot fill in full, so
	//    it is cancelled with nothing filled. At a price with enough on
	//    offer it would end "filled" instead.
	fmt.Printf("\n3. Fill-or-kill buy 1 @ %s\n", price)
	order, err = place(ctx, client, market.MarketID, price, stx.FillOrKill)
	if done := explain(err); done {
		return
	}
	fmt.Printf("Placed:  status %s  fill mode %s\n", order.Status, order.FillMode)
	final, err := demo.ReadOrder(ctx, client, order.ID, time.Minute, func(o *stx.Order) bool {
		return o.Status == "filled" || o.Status == "cancelled"
	})
	if err != nil {
		log.Fatal(err)
	}
	switch final.Status {
	case "filled":
		fmt.Printf("Filled:  %s at %s\n", final.Filled, final.AvgPrice)
	case "cancelled":
		fmt.Printf("Killed:  status cancelled  filled %s\n", filled(final.Filled))
	default:
		fmt.Printf("Final:   status %s\n", final.Status)
		cancel(ctx, client, order.ID)
	}
}

// place sends a 1-contract limit buy with a fill mode.
func place(ctx context.Context, client *stx.Client, marketID string, price stx.Decimal, mode string) (*stx.Order, error) {
	return client.Orders.Place(ctx, stx.OrderRequest{
		MarketID:      marketID,
		Action:        stx.Buy,
		OrderType:     stx.Limit,
		Price:         price,
		Quantity:      "1",
		ClientOrderID: "demo-" + rand.Text(),
		FillMode:      mode,
	})
}

// explain prints why an order was not placed and reports whether the program
// should stop. A nil error prints nothing.
func explain(err error) bool {
	var apiErr *stx.APIError
	switch {
	case err == nil:
		return false
	case errors.Is(err, stx.ErrNotAvailable):
		// The exchange has not enabled this fill mode and refused the order.
		fmt.Printf("Refused: %v\n", err)
		return true
	case errors.As(err, &apiErr):
		fmt.Fprintf(os.Stderr, "Order refused (%d): %s\n", apiErr.StatusCode, apiErr.Message)
		os.Exit(1)
	}
	log.Fatal(err)
	return true
}

func cancel(ctx context.Context, client *stx.Client, orderID string) {
	c, err := client.Orders.Cancel(ctx, orderID)
	if err != nil {
		log.Fatal(err)
	}
	done, err := demo.ReadOrder(ctx, client, orderID, time.Minute, func(o *stx.Order) bool { return o.Status == "cancelled" })
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Cancel:  %s  %s, final status %s\n", c.OrderID, c.Status, done.Status)
}

// findOffer returns an upcoming market with an offer and its best (lowest)
// offer price.
func findOffer(ctx context.Context, client *stx.Client, sport string) (stx.Market, stx.Decimal, bool, error) {
	q := &stx.MarketsQuery{Status: []string{"open"}, Trading: stx.Bool(true)}
	if sport != "" {
		q.Sports = []string{sport}
	}
	seen := 0
	for m, err := range client.Markets.All(ctx, q) {
		if err != nil {
			return stx.Market{}, "", false, err
		}
		if seen++; seen > 1000 {
			break
		}
		if !demo.IsUpcoming(m, time.Now()) || m.EventType == "combo" {
			continue
		}
		var best stx.Decimal
		for _, o := range m.Offers {
			if best == "" || o.Price.Cmp(best) < 0 {
				best = o.Price
			}
		}
		if best != "" {
			return m, best, true, nil
		}
	}
	return stx.Market{}, "", false, nil
}

func filled(d stx.Decimal) stx.Decimal {
	if d == "" {
		return "0"
	}
	return d
}
