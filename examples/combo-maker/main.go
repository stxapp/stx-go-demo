// Combo-maker quotes every combo request as a maker, and reports how each
// one ends.
//
// Players ask for quotes on a combo: a set of legs and a stake to buy, or a
// combo they hold to cash out. Every maker joined to its combo_quote topic
// receives each request. This program quotes all of them and prints what
// happens next: your quote placed (quote_accepted, then placed), another
// maker's placed instead (placed_elsewhere), your quote expiring, and the
// request closing. It needs a read_write API key.
//
// On a buy you quote what the player wins: the stake times WIN_MULTIPLE. On a
// cash out you quote what you pay for everything the player sells:
// CASH_OUT_PRICE a contract. Each quote stands for QUOTE_SECONDS. With PLAYER
// set to a player's external_id, it quotes only that player's requests.
//
//	go run ./examples/combo-maker           (quote until Ctrl-C)
//	go run ./examples/combo-maker 120       (stop after 120 s)
//	WIN_MULTIPLE=12 CASH_OUT_PRICE=0.10 QUOTE_SECONDS=60 go run ./examples/combo-maker
package main

import (
	"context"
	"errors"
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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if len(os.Args) > 1 {
		n, err := strconv.Atoi(os.Args[1])
		if err != nil {
			log.Fatalf("usage: combo-maker [seconds], not %q", os.Args[1])
		}
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(n)*time.Second)
		defer cancel()
	}
	winMultiple := stx.Decimal(envOr("WIN_MULTIPLE", "9"))
	cashOutPrice := stx.Decimal(envOr("CASH_OUT_PRICE", "0.08"))
	quoteSeconds, err := strconv.ParseFloat(envOr("QUOTE_SECONDS", "30"), 64)
	if err != nil {
		log.Fatalf("QUOTE_SECONDS is a number of seconds, not %q", os.Getenv("QUOTE_SECONDS"))
	}
	standFor := time.Duration(quoteSeconds * float64(time.Second))
	player := os.Getenv("PLAYER") // a player's external_id: quote only their requests

	client, err := stx.New()
	if err != nil {
		log.Fatal(err)
	}
	// The stream signs the socket, sends heartbeats and reconnects. Quotes
	// belong to the connection that placed them: a drop withdraws them, so
	// there is nothing to clean up.
	maker, err := client.Combos.Maker(ctx, stx.WithOnReconnect(func() {
		fmt.Println("[reconnected]  quotes from before the drop were withdrawn")
	}))
	if errors.Is(err, stx.ErrNotAvailable) {
		fmt.Printf("Combos are not available on %s.\n", client.BaseURL())
		os.Exit(1)
	}
	if err != nil {
		log.Fatal(err)
	}
	defer maker.Close()

	fmt.Printf("Quoting combos on %s as a maker.\n", client.BaseURL())
	fmt.Printf("Buys: win %sx the stake. Cash outs: %s a contract. Each quote stands %s. Ctrl-C to stop.\n\n", winMultiple, cashOutPrice, standFor)

	quoting := map[string]bool{} // with PLAYER set, the requests this maker takes part in
	for {
		var ev stx.ComboEvent
		select {
		case <-ctx.Done():
			stopped(ctx)
			return
		case e, ok := <-maker.Events():
			if !ok {
				if ctx.Err() != nil {
					stopped(ctx)
					return
				}
				log.Fatalf("the maker connection ended: %v", maker.Err())
			}
			ev = e
		}
		rfq := short(ev.RFQID)
		if player != "" {
			if o, ok := ev.Data.(*stx.ComboRFQOpenedEvent); ok && o.ExternalID == player {
				quoting[ev.RFQID] = true
			}
			if !quoting[ev.RFQID] {
				continue // another player's request
			}
		}
		switch d := ev.Data.(type) {
		case *stx.ComboRFQOpenedEvent:
			// Every request opened after the join, from every player, your own included.
			fmt.Printf("[request]  %s  %s\n", rfq, describe(d))
			quote(ctx, maker, d, winMultiple, cashOutPrice, standFor)
		case *stx.ComboQuoteAcceptedEvent:
			// The player took your quote; the exchange is placing both orders now.
			fmt.Printf("[accepted] %s  quote %s\n", rfq, d.QuoteID)
		case *stx.ComboMakerPlacedEvent:
			fmt.Printf("[won]      %s  order %s  %s @ %s  combo %s\n", rfq, d.OrderID, d.Quantity, d.Price, d.MarketID)
		case *stx.ComboQuoteClosedEvent:
			// One of your quotes stopped standing, once per quote, with the reason.
			label := "[ended]  "
			switch d.Reason {
			case "placed_elsewhere":
				label = "[lost]   "
			case "expired":
				label = "[expired]"
			}
			fmt.Printf("%s  %s  quote %s  %s\n", label, rfq, d.QuoteID, d.Reason)
		case *stx.ComboRFQClosedEvent:
			fmt.Printf("[closed]   %s  %s\n", rfq, d.Reason)
		}
	}
}

// stopped reports a stop by Ctrl-C; a run that reaches its time limit ends quietly.
func stopped(ctx context.Context) {
	if errors.Is(ctx.Err(), context.Canceled) {
		fmt.Println("\nStopped. Quotes still standing were withdrawn with the connection.")
	}
}

func describe(r *stx.ComboRFQOpenedEvent) string {
	if r.Side == "buy" {
		legs := ""
		for i, leg := range r.Legs {
			if i > 0 {
				legs += ", "
			}
			name := leg.ShortName
			if name == "" {
				name = short(leg.MarketID)
			}
			legs += fmt.Sprintf("%s %s", name, leg.Outcome)
		}
		return fmt.Sprintf("buy, stake %s, %d legs: %s", r.Amount, len(r.Legs), legs)
	}
	return fmt.Sprintf("cash out of %s on combo %s", r.Quantity, short(r.MarketID))
}

func quote(ctx context.Context, maker *stx.ComboMaker, r *stx.ComboRFQOpenedEvent, winMultiple, cashOutPrice stx.Decimal, standFor time.Duration) {
	rfq := short(r.RFQID)
	amount, ok, err := demo.MakerQuoteAmount(demo.QuoteRequest{
		Side: r.Side, Amount: r.Amount, Quantity: r.Quantity, MaxPrice: r.MaxPrice, TickSize: r.TickSize,
	}, winMultiple, cashOutPrice)
	if err != nil || !ok {
		fmt.Printf("[skip]     %s  no amount prices inside 0 to %s\n", rfq, r.MaxPrice)
		return
	}
	q, err := maker.Quote(ctx, stx.ComboQuoteRequest{
		RFQID: r.RFQID, Amount: amount, ExpiresIn: standFor, ClientOrderID: "maker-" + rfq,
	})
	var refused *stx.ComboRefusedError
	if errors.As(err, &refused) {
		// e.g. cannot_quote_own_request, or not_found when the request ended first
		fmt.Printf("[refused]  %s  %s\n", rfq, refused.Code)
		return
	}
	if err != nil {
		log.Printf("[error]    %s  %v", rfq, err)
		return
	}
	what := "win " + q.Amount.String()
	if r.Side != "buy" {
		what = fmt.Sprintf("pay %s for %s", q.Amount, r.Quantity)
	}
	fmt.Printf("[quoted]   %s  %s  for %s  quote %s\n", rfq, what, standFor, q.QuoteID)
}

func short(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
