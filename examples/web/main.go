// Web is a small net/http backend in front of the SDK: market data and
// orders through the REST client, and your account kept live by one
// WebSocket and streamed to the browser with server-sent events.
//
//	go run ./examples/web            # then open http://localhost:8080
//	ADDR=127.0.0.1:9000 go run ./examples/web
//
// Endpoints:
//
//	GET    /                   a page that shows your account as it changes
//	GET    /account/stream     server-sent events: "account" with the whole account, on connect and on every change
//	GET    /account            balance, open orders, fills and positions, kept live by the socket
//	GET    /me                 the key's user and scope
//	GET    /markets?limit=10   open markets with best bid and offer
//	GET    /markets/{id}       one market, in full
//	GET    /orders             your working orders
//	POST   /orders             place an order: {"market_id": "...", "action": "buy", "order_type": "limit", "price": "0.01", "quantity": "1"}
//	DELETE /orders/{id}        cancel an order
package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	stx "github.com/stxapp/stx-go"
	"github.com/stxapp/stx-go-demo/internal/demo"
)

//go:embed index.html
var indexHTML []byte

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := run(logger); err != nil {
		logger.Error("stopped", "err", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// One client for the whole app: it is safe for concurrent use and holds
	// the connection pool. It reads your key from ~/.stx/credentials or
	// STX_* environment variables.
	client, err := stx.New(stx.WithLogger(logger))
	if err != nil {
		return err
	}

	// One socket for the app's lifetime, with a live account view on it. The
	// stream reconnects and rejoins by itself, and the view starts over from
	// fresh snapshots each time.
	stream, err := client.Stream(ctx, stx.WithOnReconnect(func() {
		logger.Info("socket reconnected; the account view reloads from fresh snapshots")
	}))
	if err != nil {
		return err
	}
	defer stream.Close()
	view, err := stream.AccountView(ctx, stx.AccountViewOptions{})
	if err != nil {
		return err
	}
	feed := newFeed(view)
	go feed.run(logger)
	st := view.Snapshot()
	logger.Info("account view ready", "open_orders", len(st.OpenOrders), "positions", len(st.Positions))

	app := &app{client: client, view: view, feed: feed}
	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = "localhost:8080"
	}
	srv := &http.Server{Addr: addr, Handler: app.routes(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	logger.Info("listening", "url", "http://"+addr, "exchange", client.BaseURL())
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

type app struct {
	client *stx.Client
	view   *stx.AccountView
	feed   *feed
}

func (a *app) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(indexHTML)
	})
	mux.HandleFunc("GET /account/stream", a.accountStream)
	mux.HandleFunc("GET /account", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, accountJSON(a.view.Snapshot()))
	})
	mux.HandleFunc("GET /me", a.me)
	mux.HandleFunc("GET /markets", a.markets)
	mux.HandleFunc("GET /markets/{id}", a.market)
	mux.HandleFunc("GET /orders", a.orders)
	mux.HandleFunc("POST /orders", a.placeOrder)
	mux.HandleFunc("DELETE /orders/{id}", a.cancelOrder)
	return mux
}

// Who the key belongs to, and its scope.
func (a *app) me(w http.ResponseWriter, r *http.Request) {
	me, err := a.client.Account.Me(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, me)
}

// marketSummary is one line of GET /markets.
type marketSummary struct {
	MarketID        string      `json:"market_id"`
	Symbol          string      `json:"symbol"`
	Title           string      `json:"title"`
	EventTitle      string      `json:"event_title"`
	BestBid         string      `json:"best_bid"`
	BestOffer       string      `json:"best_offer"`
	LastTradedPrice stx.Decimal `json:"last_traded_price"`
	MaxPrice        stx.Decimal `json:"max_price"`
}

// Open markets with the best bid and offer.
func (a *app) markets(w http.ResponseWriter, r *http.Request) {
	limit := 10
	if s := r.URL.Query().Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > 100 {
			http.Error(w, "limit is a number from 1 to 100", http.StatusBadRequest)
			return
		}
		limit = n
	}
	page, err := a.client.Markets.List(r.Context(), &stx.MarketsQuery{Status: []string{"open"}, Trading: stx.Bool(true), Limit: limit})
	if err != nil {
		writeError(w, err)
		return
	}
	out := make([]marketSummary, 0, len(page.Items))
	for _, m := range page.Items {
		out = append(out, marketSummary{
			MarketID: m.MarketID, Symbol: m.Symbol, Title: m.Title, EventTitle: m.EventTitle,
			BestBid: demo.BestBid(m), BestOffer: demo.BestOffer(m),
			LastTradedPrice: m.LastTradedPrice, MaxPrice: m.MaxPrice,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// One market, in full.
func (a *app) market(w http.ResponseWriter, r *http.Request) {
	m, err := a.client.Markets.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, m)
}

// Your working orders.
func (a *app) orders(w http.ResponseWriter, r *http.Request) {
	page, err := a.client.Orders.List(r.Context(), &stx.OrdersQuery{Status: []string{"created", "requested", "accepted", "delayed", "open"}})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page.Items)
}

// orderBody is the body of POST /orders. Prices and quantities are decimal
// strings in and out, exactly as the exchange sends them.
type orderBody struct {
	MarketID      string      `json:"market_id"`
	Action        string      `json:"action"`
	OrderType     string      `json:"order_type"`
	Price         stx.Decimal `json:"price"`
	Quantity      stx.Decimal `json:"quantity"`
	ClientOrderID string      `json:"client_order_id"`
}

// Place an order.
func (a *app) placeOrder(w http.ResponseWriter, r *http.Request) {
	var body orderBody
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		http.Error(w, "body: "+err.Error(), http.StatusBadRequest)
		return
	}
	order, err := a.client.Orders.Place(r.Context(), stx.OrderRequest{
		MarketID: body.MarketID, Action: body.Action, OrderType: body.OrderType,
		Price: body.Price, Quantity: body.Quantity, ClientOrderID: body.ClientOrderID,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, order)
}

// Cancel an order.
func (a *app) cancelOrder(w http.ResponseWriter, r *http.Request) {
	c, err := a.client.Orders.Cancel(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

// accountStream sends the whole account as an "account" event when the
// browser connects and again after every change. A browser's EventSource
// reconnects by itself if the connection drops.
func (a *app) accountStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	changed, leave := a.feed.join()
	defer leave()
	send := func() bool {
		data, err := json.Marshal(accountJSON(a.view.Snapshot()))
		if err != nil {
			return false
		}
		if _, err := fmt.Fprintf(w, "event: account\ndata: %s\n\n", data); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}
	if !send() {
		return
	}
	keepAlive := time.NewTicker(25 * time.Second)
	defer keepAlive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-a.view.Done():
			return
		case <-keepAlive.C:
			// A comment line keeps proxies from closing an idle stream.
			if _, err := fmt.Fprint(w, ": keep-alive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case <-changed:
			if !send() {
				return
			}
		}
	}
}

// account is the JSON shape of GET /account and the "account" event.
type account struct {
	Balance    *stx.Balance   `json:"balance"`
	OpenOrders []stx.Order    `json:"open_orders"`
	Fills      []stx.Fill     `json:"fills"`
	Positions  []stx.Position `json:"positions"`
}

func accountJSON(s stx.AccountState) account {
	fills := s.Fills
	if len(fills) > 20 {
		fills = fills[:20] // the latest 20; the view keeps more
	}
	return account{Balance: s.Balance, OpenOrders: nonNil(s.OpenOrders), Fills: nonNil(fills), Positions: nonNil(s.Positions)}
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// feed fans the view's changes out to every connected browser. Each browser
// has a one-slot channel: a burst of changes coalesces into one send of the
// latest state, so a slow browser never holds up the others.
type feed struct {
	view *stx.AccountView
	mu   sync.Mutex
	subs map[chan struct{}]struct{}
}

func newFeed(view *stx.AccountView) *feed {
	return &feed{view: view, subs: map[chan struct{}]struct{}{}}
}

func (f *feed) run(logger *slog.Logger) {
	for change := range f.view.Changes() {
		if !change.Snapshot {
			logger.Info("account changed", "kind", change.Kind, "event", change.Event)
		}
		f.mu.Lock()
		for ch := range f.subs {
			select {
			case ch <- struct{}{}:
			default: // already has a pending change
			}
		}
		f.mu.Unlock()
	}
}

func (f *feed) join() (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	f.mu.Lock()
	f.subs[ch] = struct{}{}
	f.mu.Unlock()
	return ch, func() {
		f.mu.Lock()
		delete(f.subs, ch)
		f.mu.Unlock()
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError passes the exchange's status and message through, so a refused
// order comes back as the exchange explained it.
func writeError(w http.ResponseWriter, err error) {
	var apiErr *stx.APIError
	switch {
	case errors.As(err, &apiErr):
		writeJSON(w, apiErr.StatusCode, map[string]any{"error": apiErr.Message, "code": apiErr.Code})
	case errors.Is(err, stx.ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
	default:
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
	}
}
