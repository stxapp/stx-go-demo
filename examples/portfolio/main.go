// Portfolio prints your key's scope, balance, open positions, open orders and
// recent fills.
//
//	go run ./examples/portfolio
package main

import (
	"context"
	"fmt"
	"log"

	stx "github.com/stxapp/stx-go"
)

func main() {
	log.SetFlags(0)
	ctx := context.Background()
	client, err := stx.New()
	if err != nil {
		log.Fatal(err)
	}

	me, err := client.Account.Me(ctx)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Key scope: %s\n\n", me.Scope)

	// Money is dollars, as exact decimal strings. AvailableBalance is what
	// you can place orders with right now; the rest is held by open orders
	// and positions.
	balance, err := client.Account.Balance(ctx)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Balance: %s  available %s\n", balance.AccountBalance, balance.AvailableBalance)

	// Position is positive when you are long (you bought), negative when
	// short. Markets you have traded out of stay in the list with position 0;
	// skip those.
	positions, err := client.Account.Positions(ctx, nil)
	if err != nil {
		log.Fatal(err)
	}
	var open []stx.Position
	for _, p := range positions {
		if !p.Position.IsZero() {
			open = append(open, p)
		}
	}
	fmt.Printf("\nOpen positions: %d\n", len(open))
	for _, p := range open {
		fmt.Printf("  %s  %s\n", p.MarketID, p.Position)
	}

	orders, err := client.Orders.List(ctx, &stx.OrdersQuery{Status: []string{"open", "delayed"}, Limit: 10})
	if err != nil {
		log.Fatal(err)
	}
	more := ""
	if orders.HasMore() {
		more = " (more available)"
	}
	fmt.Printf("\nOpen orders: %d%s\n", len(orders.Items), more)
	for _, o := range orders.Items {
		fmt.Printf("  %s  %s %s @ %s  filled %s\n", o.ID, o.Action, o.Quantity, o.Price, o.Filled)
	}

	fills, err := client.Portfolio.Fills(ctx, &stx.FillsQuery{Limit: 5})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("\nRecent fills: %d\n", len(fills.Items))
	for _, f := range fills.Items {
		fmt.Printf("  %s  %s %s @ %s\n", f.TradeID, f.Action, f.Filled, f.Price)
	}
}
