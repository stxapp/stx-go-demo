package demo

import (
	"context"
	"errors"
	"testing"
	"time"

	stx "github.com/stxapp/stx-go"
)

type fakePlayer struct {
	events  chan stx.ComboEvent
	accepts []string
	answers []error
}

func (f *fakePlayer) Events() <-chan stx.ComboEvent { return f.events }

func (f *fakePlayer) Accept(_ context.Context, rfqID, quoteID string, amount stx.Decimal) (stx.ComboPlacement, error) {
	f.accepts = append(f.accepts, quoteID+"@"+amount.String())
	if len(f.answers) > 0 {
		err := f.answers[0]
		f.answers = f.answers[1:]
		if err != nil {
			return stx.ComboPlacement{}, err
		}
	}
	return stx.ComboPlacement{MarketID: "combo", OrderID: "o1", Quantity: "17.00", Price: "0.1167"}, nil
}

func best(rfq, quote, amount string) stx.ComboEvent {
	return stx.ComboEvent{Event: "best_quote", RFQID: rfq, Data: &stx.ComboBestQuoteEvent{RFQID: rfq, ComboBestQuote: stx.ComboBestQuote{QuoteID: quote, Amount: stx.Decimal(amount)}}}
}

func TestTakeBestQuoteAcceptsAtTheExactAmount(t *testing.T) {
	p := &fakePlayer{events: make(chan stx.ComboEvent, 4)}
	p.events <- best("other", "q0", "99.00") // another request: ignored
	p.events <- best("r1", "", "")           // nothing live yet
	p.events <- best("r1", "q1", "15.14")
	var seen int
	out, err := TakeBestQuote(context.Background(), p, "r1", time.Second, func(stx.ComboBestQuote) { seen++ })
	if err != nil || !out.Placed || out.Amount != "15.14" {
		t.Fatalf("got %+v, %v", out, err)
	}
	if seen != 2 || len(p.accepts) != 1 || p.accepts[0] != "q1@15.14" {
		t.Fatalf("seen %d quotes, accepts %v", seen, p.accepts)
	}
}

func TestTakeBestQuoteFollowsAMovedPrice(t *testing.T) {
	moved := &stx.ComboRefusedError{Code: "price_moved", Best: &stx.ComboBestQuote{QuoteID: "q2", Amount: "14.00"}}
	p := &fakePlayer{events: make(chan stx.ComboEvent, 1), answers: []error{moved}}
	p.events <- best("r1", "q1", "15.14")
	out, err := TakeBestQuote(context.Background(), p, "r1", time.Second, nil)
	if err != nil || !out.Placed || out.Amount != "14.00" {
		t.Fatalf("got %+v, %v", out, err)
	}
	if len(p.accepts) != 2 || p.accepts[1] != "q2@14.00" {
		t.Fatalf("accepts %v", p.accepts)
	}
}

func TestTakeBestQuoteReportsRefusalsAndClosesAndTimeouts(t *testing.T) {
	refused := &stx.ComboRefusedError{Code: "player_refused", Detail: "nothing_to_sell"}
	p := &fakePlayer{events: make(chan stx.ComboEvent, 1), answers: []error{refused}}
	p.events <- best("r1", "q1", "1.79")
	out, err := TakeBestQuote(context.Background(), p, "r1", time.Second, nil)
	if err != nil || out.Placed || out.Reason != "player_refused (nothing_to_sell)" {
		t.Fatalf("refusal: got %+v, %v", out, err)
	}

	p = &fakePlayer{events: make(chan stx.ComboEvent, 1)}
	p.events <- stx.ComboEvent{Event: "negotiation_closed", RFQID: "r1", Data: &stx.ComboRequestClosedEvent{RFQID: "r1", Reason: "expired"}}
	out, err = TakeBestQuote(context.Background(), p, "r1", time.Second, nil)
	if err != nil || out.Placed || out.Reason != "expired" {
		t.Fatalf("closed: got %+v, %v", out, err)
	}

	p = &fakePlayer{events: make(chan stx.ComboEvent)}
	out, err = TakeBestQuote(context.Background(), p, "r1", 20*time.Millisecond, nil)
	if err != nil || out.Placed || out.Reason != "" {
		t.Fatalf("timeout: got %+v, %v", out, err)
	}

	other := errors.New("connection reset")
	p = &fakePlayer{events: make(chan stx.ComboEvent, 1), answers: []error{other}}
	p.events <- best("r1", "q1", "1.79")
	if _, err := TakeBestQuote(context.Background(), p, "r1", time.Second, nil); !errors.Is(err, other) {
		t.Fatalf("transport error: got %v", err)
	}
}

type fakeCashOut struct{ calls int }

func (f *fakeCashOut) CashOut(context.Context, string) (stx.ComboCashOut, error) {
	f.calls++
	if f.calls < 3 {
		return stx.ComboCashOut{}, &stx.ComboRefusedError{Code: "nothing_to_sell"}
	}
	return stx.ComboCashOut{RFQID: "r2", Quantity: "17.00"}, nil
}

func TestCashOutWhenHeldRetriesNothingToSell(t *testing.T) {
	f := &fakeCashOut{}
	out, err := CashOutWhenHeld(context.Background(), f, "combo", 5, time.Millisecond)
	if err != nil || out.RFQID != "r2" || f.calls != 3 {
		t.Fatalf("got %+v, %v after %d calls", out, err, f.calls)
	}
	f = &fakeCashOut{}
	if _, err := CashOutWhenHeld(context.Background(), f, "combo", 2, time.Millisecond); err == nil || f.calls != 2 {
		t.Fatalf("gave up after %d calls with %v", f.calls, err)
	}
}
