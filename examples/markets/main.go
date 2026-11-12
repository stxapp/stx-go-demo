// Markets lists upcoming markets that are open and accepting orders, with
// their best bid and offer.
//
//	go run ./examples/markets
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	stx "github.com/stxapp/stx-go"
	"github.com/stxapp/stx-go-demo/internal/demo"
)

func main() {
	log.SetFlags(0)
	ctx := context.Background()

	// Reads your API key and exchange from STX_* environment variables or
	// ~/.stx/credentials.
	client, err := stx.New()
	if err != nil {
		log.Fatal(err)
	}

	const want = 5
	shown := 0
	now := time.Now()
	// Status "open": markets that have opened for trading (not scheduled,
	// closed or settled). Trading true: only markets accepting orders right
	// now (an open market can be paused). Markets come soonest event first.
	// All walks every page for you; List returns one page at a time instead.
	query := &stx.MarketsQuery{Status: []string{"open"}, Trading: stx.Bool(true)}
	for market, err := range client.Markets.All(ctx, query) {
		if err != nil {
			log.Fatal(err)
		}
		// Skip events already under way or past their start time: an open
		// market can outlive its event's start, and nobody quotes it then.
		if !demo.IsUpcoming(market, now) {
			continue
		}
		// Bids are what buyers will pay, offers what sellers will accept.
		// DescribeMarket prints the best of each: the highest bid and the
		// lowest offer. Prices are dollars per contract; a winning contract
		// pays market.MaxPrice.
		fmt.Println(demo.DescribeMarket(market))
		if shown++; shown >= want {
			break
		}
	}
	fmt.Printf("\n%d upcoming markets shown.\n", shown)
}
