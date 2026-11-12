# STX Go demo

[![Live](https://github.com/stxapp/stx-go-demo/actions/workflows/live.yml/badge.svg)](https://github.com/stxapp/stx-go-demo/actions/workflows/live.yml) [![CI](https://github.com/stxapp/stx-go-demo/actions/workflows/ci.yml/badge.svg)](https://github.com/stxapp/stx-go-demo/actions/workflows/ci.yml)

Small, commented Go programs that list markets, place and cancel an order, stream live prices and your account, buy and cash out a combo, quote combos as a maker, and read your portfolio on the [STX](https://stxapp.io) exchange, plus a small `net/http` backend that streams your account to the browser. They use the [`github.com/stxapp/stx-go`](https://pkg.go.dev/github.com/stxapp/stx-go) SDK.

## Two ways to use STX

- **Your own account, with an API key.** Trade and read your own STX account from your code: every example below. Start with [Setup](#setup), then `go run ./examples/markets`.
- **An app with its own users, with OAuth.** Each of your users links their STX account once, and your server then reads (or trades) their STX account for them. [`examples/link-account`](examples/link-account/main.go) is that flow in one short server; see [Link a member's STX account](#link-a-members-stx-account-oauth).

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

Each program is in [`examples/`](examples/) and runs with one command. All but the last use an API key. The sample output below comes from the US demo exchange (`region = us`, `env = demo`), and from the Ontario demo exchange (`region = ontario`, `env = demo`) where noted. Yours will show the markets open when you run it. The helpers they share (best bid and offer, picking a market, combo quote math) are in [`internal/demo/`](internal/demo/).

### List markets

`go run ./examples/markets` lists the first five markets that are open and accepting orders and whose event has not started yet, with the best bid (highest) and best offer (lowest) on each. Code: [`examples/markets/main.go`](examples/markets/main.go).

```text
Carolina Hurricanes at Seattle Kraken: NHL CAR @ SEA OU 4.5  bid 0.5000  offer 0.5500
Carolina Hurricanes at Seattle Kraken: NHL CAR @ SEA OU 7.5  bid 0.4700  offer 0.5100
Carolina Hurricanes at Seattle Kraken: NHL CAR @ SEA OU 8.5  bid 0.0600  offer 0.0700
Carolina Hurricanes at Seattle Kraken: NHL CAR @ SEA OU 5.5  bid 0.7700  offer -
Carolina Hurricanes at Seattle Kraken: NHL CAR @ SEA +2.5  bid 0.6400  offer 0.6800

5 upcoming markets shown.
```

Each line is the event, the market, then the best bid and offer. Prices are dollars per contract, as exact decimal strings (`stx.Decimal`, never a float). A bid of `0.4700` on a market whose winning contract pays `1.0000` means a buyer will pay 47 cents for a contract that pays $1 if the teams score more than 7.5 goals, and an offer of `0.5100` means a seller will take 51 cents for it. `-` means nobody is bidding or offering. Some markets pay `100.0000` for a winning contract, so their prices read like `42.0000`. Each market's `MaxPrice` says which.

`client.Markets.All` walks every page for you as an `iter.Seq2`; `client.Markets.List` returns one page at a time instead.

### Place and cancel an order

`go run ./examples/trade` places one buy order for 1 contract, reads it back, then cancels it. Code: [`examples/trade/main.go`](examples/trade/main.go).

The price is 1% of what a winning contract pays, read from the market's `MaxPrice` (one cent on a $1 contract, `1.00` on a $100 one), and the program only picks a market whose event has not started and where nobody is offering at that price or lower. The order rests on the book and does not trade, and the program cancels it before it exits.

It uses the first such market it finds. To look only at one sport, set `SPORT`, for example `SPORT=Football go run ./examples/trade`. The live example reads `SPORT` too, so the two programs still watch the same market.

```text
Market:  Carolina Hurricanes at Seattle Kraken: NHL CAR @ SEA OU 4.5
         STXNHL-26NOV121200CARSEA-TOTAL4.5  (a winning contract pays 1.0000)
Placed:  buy 1 @ 0.01  id fc93bdf8-7b8b-41c7-b026-f4d0c9fd9a74  status accepted
Read:    status open  filled 0.00 of 1.00  client id demo-ESCS5K3JVEM23HDBKNZWKLMLVR
Cancel:  fc93bdf8-7b8b-41c7-b026-f4d0c9fd9a74  cancelled
Final:   status cancelled  filled 0.00
```

The same program on the Ontario demo exchange:

```text
Market:  Chattanooga Mocs at Northern Kentucky Norse: NCAABW UTC @ NKU
         STXNCAABW-26NOV121100UTCNKU-GAMENKU  (a winning contract pays 1.0000)
Placed:  buy 1 @ 0.01  id dc4a31a4-325c-49cb-9ee5-de32157cc2c7  status accepted
Read:    status open  filled 0.00 of 1.00  client id demo-3T6AXI7KVFJ3FYOJJ4D53BPFAM
Cancel:  dc4a31a4-325c-49cb-9ee5-de32157cc2c7  cancelled
Final:   status cancelled  filled 0.00
```

An order read straight after a place or a cancel can trail it by a few seconds, so the program reads again, for up to a minute, until it sees the new status.

The order carries a `ClientOrderID`, your own id for it. If a call to place an order fails without an answer, look the order up by that id before placing it again; [Trading](https://docs.stxapp.io/sdks/go/trading/) explains why. The program does that lookup before it cancels, and it cancels whatever happens after the order is sent, so it never leaves an order on the book. It exits non-zero if the order is refused or does not end cancelled. A refusal is a `*stx.APIError` with the exchange's status and message.

### Stream prices and your account

`go run ./examples/live` opens one WebSocket, streams the order book of the market the trade example uses together with your live account (balance, open orders, fills and positions), and exits after 30 seconds. `go run ./examples/live 60` streams for 60. Code: [`examples/live/main.go`](examples/live/main.go).

Run `go run ./examples/trade` in a second terminal while it streams to see the order arrive on the book and on your account, and leave both when it is cancelled. Each `[book]` line shows the top of the book, so a one-cent order below the best bid arrives as a new push without changing the line. Each `[account]` line is one update, such as `[account] order buy 1.00 @ 0.0100  open  filled 0.00`. This run, `go run ./examples/live 10` with nothing else trading on the account, shows the starting state and one balance push:

```text
Watching Carolina Hurricanes at Seattle Kraken: NHL CAR @ SEA OU 4.5
[book]    best bid 50.00 @ 0.5000  best offer 50.00 @ 0.5500  (starting snapshot)
[book]    best bid 50.00 @ 0.5000  best offer 50.00 @ 0.5500
[account] available 100000.0000  open orders 0  positions 0

Streaming for 10 s. Run go run ./examples/trade in another terminal to see an order come and go.

[account] balance available 100000.0000

Closed. Open orders now: 0
```

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
curl http://localhost:8080/account
```

`GET /markets?limit=3` on the US demo exchange:

```json
[{"market_id":"43a378f0-dd01-4443-99f5-bfa1dae98840","symbol":"STXNHL-26NOV112130UTAVGK-PTSVGKJEICHEL289190-1.5","title":"J. Eichel 1.5 PTS","event_title":"Utah Mammoth at Vegas Golden Knights","best_bid":"-","best_offer":"-","last_traded_price":null,"max_price":"1.0000"},{"market_id":"2bf54b13-c405-4a85-8730-d964547d44c1","symbol":"STXNBA-26NOV112130GSHOU-TOTAL220.5","title":"NBA GS @ HOU OU 220.5","event_title":"Golden State Warriors at Houston Rockets","best_bid":"-","best_offer":"-","last_traded_price":null,"max_price":"1.0000"},{"market_id":"6d6e7f54-5b9b-4f5a-9ccc-849efd352e85","symbol":"STXNBA-26NOV112130GSHOU-TOTAL224.5","title":"NBA GS @ HOU OU 224.5","event_title":"Golden State Warriors at Houston Rockets","best_bid":"-","best_offer":"-","last_traded_price":null,"max_price":"1.0000"}]
```

`GET /account`, which is also the data of every `account` event on `/account/stream`:

```json
{"balance":{"account_balance":"100000.0000","account_id":"6a08bc1f-538b-4c5f-854d-13942c3e85bd","available_balance":"100000.0000","buy_order_liability":"0.0000","escrow":"0.0000","fee_schedule":"on_trade","loyalty_tier":"rookie","maker_factor":-0.01,"position_premium_liability":"0.0000","sell_order_liability":"0.0000","taker_factor":0.05,"total_adjustments":"100000.0000","total_deposits":"0.0000","total_fees":"0.0000","total_settlement_pnl":"0.0000","total_traded":"0.0000","total_withdrawals":"0.0000","user_id":"22f08815-779b-473a-bc43-818b872cedb8"},"open_orders":[],"fills":[],"positions":[]}
```

Prices and quantities are decimal strings in and out, exactly as the exchange sends them. A refused order comes back with the exchange's status and message.

### Buy and cash out a combo

`go run ./examples/combos` buys a small combo as a player and then cashes it out, so the run ends holding nothing. Code: [`examples/combos/main.go`](examples/combos/main.go). On an exchange without combos, the program says so and exits.

A combo is one market over two or more legs, and it pays only if every leg wins. The program picks two open markets from different events that have not started, asks makers to quote on a stake of `2.00`, prints each quote as it arrives (what you would win on that stake), and accepts the best one at its exact amount. The exchange then places your order on the combo's own market. The program cashes out the same way: it asks makers what they pay for everything you hold, accepts the best quote, and checks your position is back to zero.

It waits up to 20 seconds for each quote and says so if none arrives; `go run ./examples/combos 60` waits up to 60. Set the stake and number of legs with `STAKE=5 LEGS=3 go run ./examples/combos`, and look only at one sport with `SPORT=Football go run ./examples/combos`.

```text
Leg 1:   Will the Seattle Kraken and the Carolina Hurricanes combine to score more than 4.5 goals?  (CAR @ SEA OU 4.5)
Leg 2:   Will the George Mason Patriots defeat the Kansas City Roos?  (KC @ GMU)

Request: 0c1c5935-42c4-4559-bde2-33c82979e93e  stake 2.00
Waiting up to 30s for a quote...
[quote]  win 24.0000 on a 2.00 stake  (quote ea28b941-a395-48fc-bedc-70ae57146027)
Bought:  25.00 @ 0.0770  order ab67cd1a-6075-4657-b6e2-30e30b1dfb40  (accepted win 24.0000)
         combo 1496ee41-ae6d-43b2-b6cf-aeea518f18bc  Won / Won  (a winning contract pays 1.0000)

Cash out: 27a44d26-466d-4d72-a9f7-454cca19394b  selling 25.00
[quote]  pays 10.0000 for all 25.00  (quote 86be9801-3c2a-4d85-816a-56b19e5af209)
Sold:    25.00 @ 0.4000  order fc991e34-98ee-43d4-92de-69faeb564723  (accepted 10.0000)
Final:   position 0.00 on the combo  open orders 0
```

That run (`go run ./examples/combos 30`) was quoted by the maker example below on a second account, at `WIN_MULTIPLE=12 CASH_OUT_PRICE=0.40`: a 2.00 stake won 24.00 if both legs win, and the cash out paid 0.40 a contract.

When the best quote moves before your accept lands, the accept fails with `stx.ErrComboPriceMoved` and the live quote, and the program takes that one instead. [Combos](https://docs.stxapp.io/sdks/go/combos/) covers the player side.

### Quote combos as a maker

`go run ./examples/combo-maker` quotes every combo request on the exchange as a maker and prints how each one ends. Code: [`examples/combo-maker/main.go`](examples/combo-maker/main.go). It runs until Ctrl-C; `go run ./examples/combo-maker 120` stops after 120 seconds.

On a buy it quotes what the player wins: the stake times `WIN_MULTIPLE` (default 9). On a cash out it quotes what it pays for everything the player sells: `CASH_OUT_PRICE` a contract (default 0.08). Each quote stands for `QUOTE_SECONDS` (default 30). The exchange derives a price from each amount and refuses one whose price is not between 0 and the market's max price, so the program checks the same rule first and skips requests it cannot price. With `PLAYER` set to a player's external id, it quotes only that player's requests.

It prints one line per event: `[request]` for a new request, `[quoted]` for your quote, `[accepted]` when the player takes it, `[won]` when the exchange places your order on the combo's market at the same price and size as the player's, `[lost]` (`placed_elsewhere`) when the player takes another maker's quote, `[expired]` when your quote runs out, and `[closed]` when the request ends, whether you quoted or not. The run that quoted the combos example above, `WIN_MULTIPLE=12 CASH_OUT_PRICE=0.40 go run ./examples/combo-maker 60`, won both the buy and the cash out:

```text
Quoting combos on https://demo.stxapp.io as a maker.
Buys: win 12x the stake. Cash outs: 0.40 a contract. Each quote stands 30s. Ctrl-C to stop.

[request]  0c1c5935  buy, stake 2.0000, 2 legs: CAR @ SEA OU 4.5 won, KC @ GMU won
[quoted]   0c1c5935  win 24.0000  for 30s  quote ea28b941-a395-48fc-bedc-70ae57146027
[accepted] 0c1c5935  quote ea28b941-a395-48fc-bedc-70ae57146027
[won]      0c1c5935  order 2dad5204-a5e3-4841-b430-899889849742  25.00 @ 0.0770  combo 1496ee41-ae6d-43b2-b6cf-aeea518f18bc
[closed]   0c1c5935  placed
[request]  27a44d26  cash out of 25.00 on combo 1496ee41
[quoted]   27a44d26  pay 10.0000 for 25.00  for 30s  quote 86be9801-3c2a-4d85-816a-56b19e5af209
[accepted] 27a44d26  quote 86be9801-3c2a-4d85-816a-56b19e5af209
[won]      27a44d26  order 69985614-b1b5-42ca-ba5e-d24e0478a8ac  25.00 @ 0.4000  combo 1496ee41-ae6d-43b2-b6cf-aeea518f18bc
[closed]   27a44d26  placed
```

Other makers quote too, so expect to lose some requests. A request your own account opened is refused with `cannot_quote_own_request`. Quotes belong to the connection that placed them: on Ctrl-C, or if the connection drops, they are withdrawn. [Quote combos as a maker](https://docs.stxapp.io/websockets/combo-maker/) covers the maker side, and [Combo negotiation](https://docs.stxapp.io/websockets/channels/combo-negotiation/) documents each frame.

### Read your portfolio

`go run ./examples/portfolio` prints your key's scope, balance, open positions, open orders and recent fills. Code: [`examples/portfolio/main.go`](examples/portfolio/main.go).

```text
Key scope: read_write

Balance: 100000.0000  available 100000.0000

Open positions: 0

Open orders: 0

Recent fills: 0
```

`client.Account.Positions` also returns markets you have traded out of, with a position of `0.00`; the program leaves those out. The live example shows the same balance and positions as they change.

### Link a member's STX account (OAuth)

`go run ./examples/link-account` is the server side of an app with its own user accounts that links each user's STX account once. Code: [`examples/link-account/main.go`](examples/link-account/main.go). It needs an OAuth app registered with STX (client id, secret and redirect URI), not an API key; [Build an app](https://docs.stxapp.io/apps/) explains how to get one and how the flow works.

```bash
STX_HOST=demo.stxapp.io STX_OAUTH_CLIENT_ID=your-client-id STX_OAUTH_CLIENT_SECRET=your-client-secret \
  STX_OAUTH_REDIRECT_URI=http://127.0.0.1:8787/callback go run ./examples/link-account
```

Open http://127.0.0.1:8787/connect?user=alice in a browser, where `alice` stands for your own user id. STX asks the member to sign in and approve your app, then sends them back to `/callback`, which stores their tokens under `alice`. From then on `GET /member?user=alice` reads the member's profile and balance, with no further prompt: the client refreshes the tokens before they expire and stores the rotated refresh token. `POST /unlink?user=alice` revokes the link.

| Endpoint | What |
| --- | --- |
| `GET /connect?user=ID` | Sends your user to STX to approve the link (authorization code with PKCE) |
| `GET /callback` | Stores the member's tokens under your user id |
| `GET /member?user=ID` | The linked member's profile and balance |
| `POST /unlink?user=ID` | Revokes the link and forgets the tokens |

The client secret and every token stay on your server. The example keeps tokens in `stx-tokens.json` and takes the user from `?user=` to stay short; a real app keeps tokens encrypted in its database and takes the user from its own login session. It asks for `profile.read` and `balance.read`; add `orders.write` to trade for the member. Errors come back as what your front end should do next: `link_required`, `account_pending` (the member has not finished verifying their STX account), `terms_required` with the page to send them to, or the exchange's reason. [Apps with the Go SDK](https://docs.stxapp.io/sdks/go/apps/) covers the calls in full.

## Tests

```bash
go vet ./...
go test ./...
```

The tests cover the shared helpers with fakes, so they need no API key and make no network calls.

The [Live](.github/workflows/live.yml) workflow also runs every example, the web backend included, against the US and Ontario demo exchanges on each push to `main` and once a day.

## Documentation

- [Go SDK guide](https://docs.stxapp.io/sdks/go/)
- [API reference](https://pkg.go.dev/github.com/stxapp/stx-go)
- [Support](https://docs.stxapp.io/support/)

MIT license. Use of the STX API and exchange is subject to the STX terms of use and privacy policy: United States ([terms of use](https://stxapp.io/legal/terms-of-use.pdf), [privacy policy](https://stxapp.io/legal/privacy-policy.pdf)), Ontario ([terms of use](https://stxapp.ca/legal/terms-of-use.pdf), [privacy policy](https://stxapp.ca/legal/privacy-policy.pdf)).
