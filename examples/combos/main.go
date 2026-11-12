// Combos buys a small combo as a player, then cashes it out, over one
// WebSocket.
//
// A combo is one market over two or more legs: it pays only if every leg
// wins. You ask makers to quote on the legs and a stake, they quote what you
// would win, and accepting the best quote places an order for you on the
// combo's own market. Cashing out works the same way in reverse, so the run
// ends holding nothing. It needs a read_write API key.
//
//	go run ./examples/combos                    (a 2.00 stake on two legs; waits 20 s for each quote)
//	go run ./examples/combos 60                 (waits up to 60 s for each quote)
//	STAKE=5 LEGS=3 go run ./examples/combos     (a 5.00 stake on three legs)
//	SPORT=Football go run ./examples/combos     (legs from one sport)
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	stx "github.com/stxapp/stx-go"
	"github.com/stxapp/stx-go-demo/internal/demo"
)

func main() {
	log.SetFlags(0)
	os.Exit(run(context.Background()))
}

func run(ctx context.Context) int {
	wait := 20 * time.Second
	if len(os.Args) > 1 {
		n, err := strconv.Atoi(os.Args[1])
		if err != nil {
			log.Fatalf("usage: combos [seconds], not %q", os.Args[1])
		}
		wait = time.Duration(n) * time.Second
	}
	stake, err := demo.ComboStake(envOr("STAKE", "2"))
	if err != nil {
		log.Fatal(err)
	}
	legCount, err := strconv.Atoi(envOr("LEGS", "2"))
	if err != nil || legCount < 2 {
		log.Fatalf("LEGS is a whole number, 2 or more, not %q", os.Getenv("LEGS"))
	}

	client, err := stx.New()
	if err != nil {
		log.Fatal(err)
	}

	// 1. Join your player topic, on a socket of its own. An exchange without
	//    combos refuses the join. Closing the player closes the socket and
	//    ends any request still open.
	player, err := client.Combos.Player(ctx)
	if errors.Is(err, stx.ErrNotAvailable) {
		fmt.Printf("Combos are not available on %s.\n", client.BaseURL())
		return 1
	}
	if err != nil {
		log.Fatal(err)
	}
	defer player.Close()

	// 2. Pick the legs: open markets from different events that have not started.
	markets, err := demo.PickComboLegs(ctx, client, legCount, os.Getenv("SPORT"), 500, time.Now())
	if err != nil {
		log.Fatal(err)
	}
	if len(markets) < legCount {
		fmt.Printf("Found %d open market(s) from different events; %d legs need %d.\n", len(markets), legCount, legCount)
		return 0
	}
	legs := make([]stx.ComboLeg, len(markets))
	for i, m := range markets {
		name := m.Question
		if name == "" {
			name = m.Title
		}
		fmt.Printf("Leg %d:   %s  (%s)\n", i+1, name, m.ShortTitle)
		// Every outcome is "won": the combo pays if all the legs win.
		legs[i] = stx.ComboLeg{MarketID: m.MarketID, Outcome: stx.ComboWon}
	}

	// 3. Ask for quotes.
	req, err := player.RequestQuote(ctx, legs, stake)
	var refused *stx.ComboRefusedError
	if errors.As(err, &refused) {
		fmt.Printf("Request refused: %s\n", refused.Code) // e.g. insufficient_assets, leg_not_quotable
		return 1
	}
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("\nRequest: %s  stake %s\n", req.RFQID, stake)
	fmt.Printf("Waiting up to %s for a quote...\n", wait)

	// 4. Take the best quote at its exact amount: what you win on your stake.
	bought, err := demo.TakeBestQuote(ctx, player, req.RFQID, wait, func(q stx.ComboBestQuote) {
		if q.Amount == "" {
			fmt.Println("[quote]  none live")
			return
		}
		fmt.Printf("[quote]  win %s on a %s stake  (quote %s)\n", q.Amount, stake, q.QuoteID)
	})
	if err != nil {
		log.Fatal(err)
	}
	if !bought.Placed {
		fmt.Println(notPlaced(bought, "quote", wait))
		_ = player.Cancel(ctx, req.RFQID) // already ended if it closed
		return 1
	}
	placed := bought.Placement
	combo, err := client.Markets.Get(ctx, placed.MarketID)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Bought:  %s @ %s  order %s  (accepted win %s)\n", placed.Quantity, placed.Price, placed.OrderID, bought.Amount)
	fmt.Printf("         combo %s  %s  (a winning contract pays %s)\n", placed.MarketID, combo.ShortTitle, combo.MaxPrice)

	// 5. Cash out: a new request to sell everything you hold of the combo.
	out, err := demo.CashOutWhenHeld(ctx, player, placed.MarketID, 15, time.Second)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("\nCash out: %s  selling %s\n", out.RFQID, out.Quantity)
	sold, err := demo.TakeBestQuote(ctx, player, out.RFQID, wait, func(q stx.ComboBestQuote) {
		if q.Amount == "" {
			fmt.Println("[quote]  none live")
			return
		}
		fmt.Printf("[quote]  pays %s for all %s  (quote %s)\n", q.Amount, out.Quantity, q.QuoteID)
	})
	if err != nil {
		log.Fatal(err)
	}
	if !sold.Placed {
		fmt.Println(notPlaced(sold, "cash-out quote", wait))
		fmt.Printf("You still hold %s of combo %s. Run again later, or sell it on its order book.\n", out.Quantity, placed.MarketID)
		_ = player.Cancel(ctx, out.RFQID)
		return 1
	}
	fmt.Printf("Sold:    %s @ %s  order %s  (accepted %s)\n", sold.Placement.Quantity, sold.Placement.Price, sold.Placement.OrderID, sold.Amount)

	// 6. Confirm nothing is left: no position and no resting order on the
	//    combo. A trade shows in your position a moment after it fills, so
	//    look a few times.
	position, err := settledPosition(ctx, client, placed.MarketID)
	if err != nil {
		log.Fatal(err)
	}
	resting, err := client.Orders.List(ctx, &stx.OrdersQuery{MarketIDs: []string{placed.MarketID}, Status: []string{"open", "delayed"}})
	if err != nil {
		log.Fatal(err)
	}
	for _, o := range resting.Items {
		// The unfilled rest of an order the maker did not fully take.
		if _, err := client.Orders.Cancel(ctx, o.ID); err != nil {
			log.Fatal(err)
		}
	}
	left := "0"
	if n := len(resting.Items); n > 0 {
		left = fmt.Sprintf("%d, cancelled", n)
	}
	fmt.Printf("Final:   position %s on the combo  open orders %s\n", position, left)
	if !position.IsZero() {
		return 1
	}
	return 0
}

// settledPosition is your position in a market, read until it is flat or a
// minute passes.
func settledPosition(ctx context.Context, client *stx.Client, marketID string) (stx.Decimal, error) {
	position := stx.Decimal("0.00")
	for range 30 {
		held, err := client.Account.Positions(ctx, &stx.PositionsQuery{MarketIDs: []string{marketID}})
		if err != nil {
			return "", err
		}
		position = "0.00"
		if len(held) > 0 && held[0].Position != "" {
			position = held[0].Position
		}
		if position.IsZero() {
			break
		}
		time.Sleep(2 * time.Second)
	}
	return position, nil
}

func notPlaced(o demo.QuoteOutcome, what string, wait time.Duration) string {
	if o.Reason != "" {
		return fmt.Sprintf("The request ended without a trade: %s.", o.Reason)
	}
	return fmt.Sprintf("No %s arrived within %s. Makers quote combos as they choose; try again, or wait longer with go run ./examples/combos 60.", what, wait)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
