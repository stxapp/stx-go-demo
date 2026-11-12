# STX Go demo

[![Live](https://github.com/stxapp/stx-go-demo/actions/workflows/live.yml/badge.svg)](https://github.com/stxapp/stx-go-demo/actions/workflows/live.yml) [![CI](https://github.com/stxapp/stx-go-demo/actions/workflows/ci.yml/badge.svg)](https://github.com/stxapp/stx-go-demo/actions/workflows/ci.yml)

Small, commented Go programs that list markets, place and cancel an order, stream live prices and your account, buy and cash out a combo, quote combos as a maker, and read your portfolio on the [STX](https://stxapp.io) exchange, plus a small `net/http` backend that streams your account to the browser. They use the [`github.com/stxapp/stx-go`](https://pkg.go.dev/github.com/stxapp/stx-go) SDK.

## Before you start

- Go 1.24 or later.
- An API key for the STX exchange you are building against. [Environments](https://docs.stxapp.io/environments/) lists the exchanges and where to get a key. Start on a demo exchange. Placing orders needs a `read_write` key.

## Setup

```bash
git clone https://github.com/stxapp/stx-go-demo.git
cd stx-go-demo
go build ./...
```

Give the programs your API key in one of two ways. [Authentication](https://docs.stxapp.io/sdks/go/authentication/) covers both in full.

A profile in `~/.stx/credentials`, with the region and env of your exchange from [Environments](https://docs.stxapp.io/environments/):

```ini
[default]
region   = us
env      = demo
key_id   = your-key-id
key_file = ~/.stx/stx-key.pem
```

To keep several profiles, name each section and pick one with `STX_PROFILE`, for example `STX_PROFILE=my-demo go run ./examples/markets`.

Or environment variables:

```bash
export STX_REGION=us STX_ENV=demo
export STX_KEY_ID=your-key-id
export STX_PRIVATE_KEY=~/.stx/stx-key.pem   # a path, or the PEM text
```

The programs never print your key.

## Examples

Each program is in [`examples/`](examples/) and runs with one command. The helpers they share (best bid and offer, picking a market, combo quote math) are in [`internal/demo/`](internal/demo/).

### List markets

`go run ./examples/markets` lists the first five markets that are open and accepting orders and whose event has not started yet, with the best bid (highest) and best offer (lowest) on each. Code: [`examples/markets/main.go`](examples/markets/main.go).

Each line reads `Manchester United at Como: UEFA MAN @ COMO Draw 1st Half  bid 0.3100  offer 0.3600`: the event, the market, then the best bid and offer.

Prices are dollars per contract, as exact decimal strings (`stx.Decimal`, never a float). A bid of `0.3100` on a market whose winning contract pays `1.0000` means a buyer will pay 31 cents for a contract that pays $1 if the first half is a draw, and an offer of `0.3600` means a seller will take 36 cents for it. `-` means nobody is bidding or offering. Some markets pay `100.0000` for a winning contract, so their prices read like `42.0000`. Each market's `MaxPrice` says which.

`client.Markets.All` walks every page for you as an `iter.Seq2`; `client.Markets.List` returns one page at a time instead.

### Place and cancel an order

`go run ./examples/trade` places one buy order for 1 contract, reads it back, then cancels it. Code: [`examples/trade/main.go`](examples/trade/main.go).

The price is 1% of what a winning contract pays, read from the market's `MaxPrice` (one cent on a $1 contract, `1.00` on a $100 one), and the program only picks a market whose event has not started and where nobody is offering at that price or lower. The order rests on the book and does not trade, and the program cancels it before it exits.

It uses the first such market it finds. To look only at one sport, set `SPORT`, for example `SPORT=Football go run ./examples/trade`. The live example reads `SPORT` too, so the two programs still watch the same market.

It prints the market, then one line per step: `Placed:` with the order id and status `accepted`, `Read:` with status `open` and `filled 0.00 of 1.00`, `Cancel:` with `cancelled`, and `Final:   status cancelled  filled 0.00`.

The order carries a `ClientOrderID`, your own id for it. If a call to place an order fails without an answer, look the order up by that id before placing it again; [Trading](https://docs.stxapp.io/sdks/go/trading/) explains why. The program does that lookup before it cancels, and it cancels whatever happens after the order is sent, so it never leaves an order on the book. It exits non-zero if the order is refused or does not end cancelled. A refusal is a `*stx.APIError` with the exchange's status and message.

### Stream prices and your account

`go run ./examples/live` opens one WebSocket, streams the order book of the market the trade example uses together with your live account (balance, open orders, fills and positions), and exits after 30 seconds. `go run ./examples/live 60` streams for 60. Code: [`examples/live/main.go`](examples/live/main.go).

Run `go run ./examples/trade` in a second terminal while it streams to see the order arrive on the book and on your account, and leave both when it is cancelled. Each `[book]` line shows the top of the book, so a one-cent order below the best bid arrives as a new push without changing the line. Each `[account]` line is one update, such as `[account] order buy 1.00 @ 0.0100  open  filled 0.00`.

`client.Stream` signs the connection, sends heartbeats, reconnects and rejoins for you. `stream.AccountView` returns once the starting snapshot of each part is in, then keeps itself current; `view.Changes()` reports every update and `view.Snapshot()` reads the whole account at any time. The channels and their payloads are described under [WebSockets](https://docs.stxapp.io/sdks/go/websockets/).

### Stream your account to a browser

`go run ./examples/web` starts a small `net/http` backend on http://localhost:8080 (set `ADDR` to change it). Open it in a browser to see your balance, open orders, positions and recent fills update as they change. Code: [`examples/web/main.go`](examples/web/main.go).

The backend keeps one WebSocket and one live account view for its whole life, so the browser never waits on a call to the exchange, and it pushes the account to every open page with [server-sent events](https://developer.mozilla.org/en-US/docs/Web/API/Server-sent_events): an `account` event with the whole account when the page connects and again after every change. It also serves the REST calls an app usually needs:

| Endpoint | What |
| --- | --- |
| `GET /account/stream` | Server-sent events: `account`, on connect and on every change |
| `GET /account` | Balance, open orders, fills and positions, kept live by the socket |
| `GET /me` | The key's user and scope |
| `GET /markets?limit=10` | Open markets with best bid and offer |
| `GET /markets/{id}` | One market, in full |
| `GET /orders` | Your working orders |
| `POST /orders` | Place an order: `{"market_id": "...", "action": "buy", "order_type": "limit", "price": "0.01", "quantity": "1"}` |
| `DELETE /orders/{id}` | Cancel an order |

```bash
curl -N http://localhost:8080/account/stream
curl 'http://localhost:8080/markets?limit=3'
```

Prices and quantities are decimal strings in and out, exactly as the exchange sends them. A refused order comes back with the exchange's status and message.

### Buy and cash out a combo

`go run ./examples/combos` buys a small combo as a player and then cashes it out, so the run ends holding nothing. Code: [`examples/combos/main.go`](examples/combos/main.go). On an exchange without combos, the program says so and exits.

A combo is one market over two or more legs, and it pays only if every leg wins. The program picks two open markets from different events that have not started, asks makers to quote on a stake of `2.00`, prints each quote as it arrives (what you would win on that stake), and accepts the best one at its exact amount. The exchange then places your order on the combo's own market. The program cashes out the same way: it asks makers what they pay for everything you hold, accepts the best quote, and checks your position is back to zero.

It waits up to 20 seconds for each quote and says so if none arrives; `go run ./examples/combos 60` waits up to 60. Set the stake and number of legs with `STAKE=5 LEGS=3 go run ./examples/combos`, and look only at one sport with `SPORT=Football go run ./examples/combos`.

When the best quote moves before your accept lands, the accept fails with `stx.ErrComboPriceMoved` and the live quote, and the program takes that one instead. [Combos](https://docs.stxapp.io/sdks/go/combos/) covers the player side.

### Quote combos as a maker

`go run ./examples/combo-maker` quotes every combo request on the exchange as a maker and prints how each one ends. Code: [`examples/combo-maker/main.go`](examples/combo-maker/main.go). It runs until Ctrl-C; `go run ./examples/combo-maker 120` stops after 120 seconds.

On a buy it quotes what the player wins: the stake times `WIN_MULTIPLE` (default 9). On a cash out it quotes what it pays for everything the player sells: `CASH_OUT_PRICE` a contract (default 0.08). Each quote stands for `QUOTE_SECONDS` (default 30). The exchange derives a price from each amount and refuses one whose price is not between 0 and the market's max price, so the program checks the same rule first and skips requests it cannot price. With `PLAYER` set to a player's external id, it quotes only that player's requests.

It prints one line per event: `[request]` for a new request, `[quoted]` for your quote, `[accepted]` when the player takes it, `[won]` when the exchange places your order on the combo's market at the same price and size as the player's, `[lost]` (`placed_elsewhere`) when the player takes another maker's quote, `[expired]` when your quote runs out, and `[closed]` when the request ends, whether you quoted or not. Other makers quote too, so expect to lose some requests. A request your own account opened is refused with `cannot_quote_own_request`. Quotes belong to the connection that placed them: on Ctrl-C, or if the connection drops, they are withdrawn. [Quote combos as a maker](https://docs.stxapp.io/websockets/combo-maker/) covers the maker side, and [Combo negotiation](https://docs.stxapp.io/websockets/channels/combo-negotiation/) documents each frame.

### Read your portfolio

`go run ./examples/portfolio` prints your key's scope, balance, open positions, open orders and recent fills. Code: [`examples/portfolio/main.go`](examples/portfolio/main.go).

`client.Account.Positions` also returns markets you have traded out of, with a position of `0.00`; the program leaves those out. The live example shows the same balance and positions as they change.

## Tests

```bash
go vet ./...
go test ./...
```

The tests cover the shared helpers with fakes, so they need no API key and make no network calls.

The [Live](.github/workflows/live.yml) workflow also runs every example, the web backend included, against the US and Ontario demo exchanges on each push to `main` and once a day.

## Building an app for other STX members?

Everything above trades your own account with your own API key. OAuth is only needed when you build an app that acts on behalf of other STX members, with their consent. See [Apps with the Go SDK](https://docs.stxapp.io/sdks/go/apps/) and the [ISV guide](https://docs.stxapp.io/isv/), and the example app [stx-isv-demo](https://github.com/stxapp/stx-isv-demo).

## Documentation

- [Go SDK guide](https://docs.stxapp.io/sdks/go/)
- [API reference](https://pkg.go.dev/github.com/stxapp/stx-go)
- [Support](https://docs.stxapp.io/support/)

MIT license. Use of the STX API and exchange is subject to the STX terms of use and privacy policy: United States ([terms of use](https://stxapp.io/legal/terms-of-use.pdf), [privacy policy](https://stxapp.io/legal/privacy-policy.pdf)), Ontario ([terms of use](https://stxapp.ca/legal/terms-of-use.pdf), [privacy policy](https://stxapp.ca/legal/privacy-policy.pdf)).
