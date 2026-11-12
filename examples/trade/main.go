// Trade places one small buy order that will rest on the book, reads it
// back, then cancels it.
//
// The price is 1% of what a winning contract pays (one cent on a $1
// contract), and the program only uses a market where nobody is offering at
// that price or lower, so the order rests instead of trading. It needs a
// read_write API key.
//
//	go run ./examples/trade
//	SPORT=Football go run ./examples/trade   (only look at markets in that sport)
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
	os.Exit(run(context.Background()))
}

func run(ctx context.Context) int {
	client, err := stx.New()
	if err != nil {
		log.Fatal(err)
	}

	// 1. Find a market that accepts orders, whose event has not started, and
	//    a price that will rest.
	market, price, ok, err := demo.FindRestingBuy(ctx, client, os.Getenv("SPORT"), 1000, time.Now())
	if err != nil {
		log.Fatal(err)
	}
	if !ok {
		fmt.Println("No open market right now where a small buy would rest. Try again later.")
		return 0
	}
	fmt.Printf("Market:  %s: %s\n", market.EventTitle, market.Title)
	fmt.Printf("         %s  (a winning contract pays %s)\n", market.Symbol, market.MaxPrice)

	// 2. Place the order. ClientOrderID is your own id: use it to find the
	//    order again if the call fails without an answer, instead of placing
	//    it twice.
	clientOrderID := "demo-" + rand.Text()
	status := 0
	orderID := ""
	refused := false
	order, err := client.Orders.Place(ctx, stx.OrderRequest{
		MarketID:      market.MarketID,
		Action:        stx.Buy,
		OrderType:     stx.Limit,
		Price:         price, // dollars in whole cents, e.g. "0.01"
		Quantity:      "1",   // one contract
		ClientOrderID: clientOrderID,
	})
	var apiErr *stx.APIError
	switch {
	case err == nil:
		orderID = order.ID
		fmt.Printf("Placed:  buy 1 @ %s  id %s  status %s\n", price, orderID, order.Status)
		// 3. Read it back. The order is on the book and has not filled.
		open, err := client.Orders.Get(ctx, orderID)
		if err != nil {
			log.Print(err)
			status = 1
			break
		}
		fmt.Printf("Read:    status %s  filled %s of %s  client id %s\n", open.Status, open.Filled, open.Quantity, open.ClientOrderID)
	case errors.As(err, &apiErr):
		// The exchange explains why it refused the order, e.g. a read_only
		// key or a market that just paused.
		refused = true
		fmt.Fprintf(os.Stderr, "Order refused (%d): %s\n", apiErr.StatusCode, apiErr.Message)
		status = 1
	default:
		// No answer: the order may or may not exist. Look it up below.
		fmt.Fprintf(os.Stderr, "Placing failed without an answer: %v\n", err)
		status = 1
	}

	// 4. Cancel it, whatever happened above. If placing failed without an
	//    answer the order may still exist, so look it up by ClientOrderID.
	if orderID == "" && !refused {
		found, err := client.Orders.List(ctx, &stx.OrdersQuery{ClientOrderIDs: []string{clientOrderID}})
		if err != nil {
			log.Fatal(err)
		}
		if len(found.Items) > 0 {
			orderID = found.Items[0].ID
		}
	}
	if orderID == "" {
		return status
	}
	cancellation, err := client.Orders.Cancel(ctx, orderID)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Cancel:  %s  %s\n", cancellation.OrderID, cancellation.Status)

	// 5. Confirm the final state.
	done, err := client.Orders.Get(ctx, orderID)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Final:   status %s  filled %s\n", done.Status, done.Filled)
	if done.Status != "cancelled" {
		return 1
	}
	return status
}
