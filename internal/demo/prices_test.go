package demo

import (
	"testing"
	"time"

	stx "github.com/stxapp/stx-go"
)

func level(price string) stx.BookLevel {
	return stx.BookLevel{Price: stx.Decimal(price), Quantity: "1.00"}
}

func TestBestLevelPicksByPrice(t *testing.T) {
	levels := []stx.BookLevel{level("0.3600"), level("0.4000"), level("0.3100")}
	if l, _ := BestLevel(levels, true); l.Price != "0.4000" {
		t.Errorf("best bid %s, want 0.4000", l.Price)
	}
	if l, _ := BestLevel(levels, false); l.Price != "0.3100" {
		t.Errorf("best offer %s, want 0.3100", l.Price)
	}
	if _, ok := BestLevel(nil, true); ok {
		t.Error("empty side has a best level")
	}
	m := stx.Market{Bids: []stx.BookLevel{level("0.3100")}}
	if BestBid(m) != "0.3100" || BestOffer(m) != "-" {
		t.Errorf("BestBid %s BestOffer %s", BestBid(m), BestOffer(m))
	}
}

func TestIsUpcoming(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		status string
		start  time.Time
		want   bool
	}{
		{"scheduled", now.Add(time.Hour), true},
		{"", now.Add(time.Hour), true},
		{"scheduled", now.Add(-time.Hour), false},
		{"live", now.Add(time.Hour), false},
		{"scheduled", time.Time{}, false},
	}
	for _, c := range cases {
		m := stx.Market{EventStatus: c.status, EventStart: c.start}
		if got := IsUpcoming(m, now); got != c.want {
			t.Errorf("IsUpcoming(%q, %v) = %v, want %v", c.status, c.start, got, c.want)
		}
	}
}

func TestRestingBuyPrice(t *testing.T) {
	for in, want := range map[string]string{"1.0000": "0.01", "100.0000": "1.00", "0.50": "0.01"} {
		got, err := RestingBuyPrice(stx.Decimal(in))
		if err != nil || got != stx.Decimal(want) {
			t.Errorf("RestingBuyPrice(%s) = %s, %v; want %s", in, got, err, want)
		}
	}
	if _, err := RestingBuyPrice("0.01"); err == nil {
		t.Error("RestingBuyPrice(0.01) did not fail")
	}
}

func TestWouldRest(t *testing.T) {
	if !WouldRest(stx.Market{}, "0.01") {
		t.Error("empty book: should rest")
	}
	if !WouldRest(stx.Market{Offers: []stx.BookLevel{level("0.0200")}}, "0.01") {
		t.Error("offer above: should rest")
	}
	if WouldRest(stx.Market{Offers: []stx.BookLevel{level("0.0100")}}, "0.01") {
		t.Error("offer at the price: should trade")
	}
}

func TestComboStake(t *testing.T) {
	for in, want := range map[string]string{"2": "2.00", "2.5": "2.50", " 5.25 ": "5.25"} {
		got, err := ComboStake(in)
		if err != nil || got != stx.Decimal(want) {
			t.Errorf("ComboStake(%q) = %s, %v; want %s", in, got, err, want)
		}
	}
	for _, in := range []string{"0", "5.001", "-1", "five"} {
		if _, err := ComboStake(in); err == nil {
			t.Errorf("ComboStake(%q) did not fail", in)
		}
	}
}

func TestMakerQuoteAmountBuy(t *testing.T) {
	buy := QuoteRequest{Side: "buy", Amount: "2.0000", MaxPrice: "1.0000", TickSize: "0.0001000"}
	check := func(req QuoteRequest, mult string, want string) {
		t.Helper()
		got, ok, err := MakerQuoteAmount(req, stx.Decimal(mult), "0.08")
		if err != nil {
			t.Fatal(err)
		}
		if want == "" && ok {
			t.Errorf("multiple %s: got %s, want no quote", mult, got)
		}
		if want != "" && (!ok || got != stx.Decimal(want)) {
			t.Errorf("multiple %s: got %s (%v), want %s", mult, got, ok, want)
		}
	}
	check(buy, "9", "18.00")
	b := buy
	b.Amount = "2.5000"
	check(b, "1.333", "3.33")
	// 0.50 / (0.50 + 0.01) prices at 0.9804: 0.50 buys no whole contract.
	b.Amount = "0.5000"
	check(b, "0.02", "")
	check(buy, "0", "")
}

func TestMakerQuoteAmountCashOut(t *testing.T) {
	sell := QuoteRequest{Side: "sell", Quantity: "17.00", MaxPrice: "1.0000", TickSize: "0.0001000"}
	for price, want := range map[string]string{"0.08": "1.36", "1.00": "", "1.50": ""} {
		got, ok, err := MakerQuoteAmount(sell, "9", stx.Decimal(price))
		if err != nil {
			t.Fatal(err)
		}
		if (want == "" && ok) || (want != "" && got != stx.Decimal(want)) {
			t.Errorf("cash out at %s: got %s (%v), want %q", price, got, ok, want)
		}
	}
}
