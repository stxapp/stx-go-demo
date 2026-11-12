// Live streams a market's order book and your live account over one
// WebSocket, then exits.
//
// It watches the same market that the trade example uses, so running
// `go run ./examples/trade` in a second terminal shows the order arriving on
// both streams.
//
//	go run ./examples/live                       (streams for 30 seconds)
//	go run ./examples/live 60                    (streams for 60 seconds)
//	SPORT=Football go run ./examples/live        (watch a market in that sport, as trade does)
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"time"

	stx "github.com/stxapp/stx-go"
	"github.com/stxapp/stx-go-demo/internal/demo"
)

func main() {
	log.SetFlags(0)
	seconds := 30
	if len(os.Args) > 1 {
		n, err := strconv.Atoi(os.Args[1])
		if err != nil {
			log.Fatalf("usage: live [seconds], not %q", os.Args[1])
		}
		seconds = n
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	client, err := stx.New()
	if err != nil {
		log.Fatal(err)
	}
	market, _, ok, err := demo.FindRestingBuy(ctx, client, os.Getenv("SPORT"), 1000, time.Now())
	if err != nil {
		log.Fatal(err)
	}
	if !ok {
		fmt.Println("No open market to watch right now.")
		return
	}
	fmt.Printf("Watching %s: %s\n", market.EventTitle, market.Title)
	fmt.Printf("[book]    %s  (starting snapshot)\n", top(market.Bids, market.Offers))

	// One signed socket carries every channel. The stream sends heartbeats,
	// reconnects and rejoins for you.
	stream, err := client.Stream(ctx)
	if err != nil {
		log.Fatal(err)
	}

	// The order book channel pushes the market's whole book each time it changes.
	book, err := stream.Subscribe(ctx, stx.OrderbookChannel(market.MarketID))
	if err != nil {
		log.Fatal(err)
	}
	go func() {
		for msg := range book.Messages() {
			if b, ok := msg.Data.(*stx.OrderbookUpdate); ok {
				fmt.Printf("[book]    %s\n", top(levels(b.Bids), levels(b.Offers)))
			}
		}
	}()

	// The live account view keeps your balance, open orders, fills and
	// positions current. It returns once the starting snapshot of each is in;
	// Changes then reports every update.
	view, err := stream.AccountView(ctx, stx.AccountViewOptions{})
	if err != nil {
		log.Fatal(err)
	}
	st := view.Snapshot()
	available := "-"
	if st.Balance != nil {
		available = st.Balance.AvailableBalance.String()
	}
	fmt.Printf("[account] available %s  open orders %d  positions %d\n", available, len(st.OpenOrders), len(st.Positions))
	go func() {
		for change := range view.Changes() {
			if !change.Snapshot {
				fmt.Printf("[account] %s\n", describe(change))
			}
		}
	}()

	fmt.Printf("\nStreaming for %d s. Run go run ./examples/trade in another terminal to see an order come and go.\n\n", seconds)
	select {
	case <-time.After(time.Duration(seconds) * time.Second):
	case <-ctx.Done():
	}

	if err := stream.Close(); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("\nClosed. Open orders now: %d\n", len(view.Snapshot().OpenOrders))
}

// levels turns the order book channel's levels into the REST book's, so one
// helper picks the best of both.
func levels(in []stx.OrderbookLevel) []stx.BookLevel {
	out := make([]stx.BookLevel, len(in))
	for i, l := range in {
		out[i] = stx.BookLevel{Price: l.Price, Quantity: l.Quantity}
	}
	return out
}

// top is the best bid and best offer with their sizes. The best bid is the
// highest price and the best offer the lowest, whatever order the levels
// arrive in.
func top(bids, offers []stx.BookLevel) string {
	side := func(levels []stx.BookLevel, bid bool) string {
		if l, ok := demo.BestLevel(levels, bid); ok {
			return fmt.Sprintf("%s @ %s", l.Quantity, l.Price)
		}
		return "-"
	}
	return fmt.Sprintf("best bid %s  best offer %s", side(bids, true), side(offers, false))
}

// describe is one line per account update. The payloads are documented per
// channel on docs.stxapp.io.
func describe(c stx.AccountChange) string {
	switch d := c.Data.(type) {
	case *stx.Order:
		return fmt.Sprintf("order %s %s @ %s  %s  filled %s", d.Action, d.Quantity, d.Price, d.Status, d.Filled)
	case *stx.Fill:
		return fmt.Sprintf("fill %s %s @ %s", d.Action, d.Filled, d.Price)
	case *stx.Balance:
		return fmt.Sprintf("balance available %s", d.AvailableBalance)
	case *stx.PositionsUpdate:
		return fmt.Sprintf("positions %d updated", len(d.Positions))
	}
	return fmt.Sprintf("%s %s", c.Kind, c.Event)
}
