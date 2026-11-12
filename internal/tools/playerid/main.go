// Command playerid prints the external_id of the key's account: the one
// identity a combo maker sees on that player's requests. The Live workflow
// passes it to combo-maker as PLAYER.
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	stx "github.com/stxapp/stx-go"
)

func main() {
	log.SetFlags(0)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, err := stx.New()
	if err != nil {
		log.Fatal(err)
	}
	player, err := client.Combos.Player(ctx)
	if err != nil {
		log.Fatal(err)
	}
	defer player.Close()
	fmt.Println(player.ExternalID())
}
