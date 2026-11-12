package demo

import (
	"context"
	"errors"
	"fmt"
	"time"

	stx "github.com/stxapp/stx-go"
)

// QuotePlayer is what TakeBestQuote needs of a [stx.ComboPlayer].
type QuotePlayer interface {
	Events() <-chan stx.ComboEvent
	Accept(ctx context.Context, rfqID, quoteID string, amount stx.Decimal) (stx.ComboPlacement, error)
}

// QuoteOutcome is how a request ended for TakeBestQuote.
type QuoteOutcome struct {
	// Placed is true when a quote was accepted; Amount and Placement say
	// what was taken.
	Placed    bool
	Amount    stx.Decimal
	Placement stx.ComboPlacement
	// Reason is why the request closed without a trade, or empty when no
	// quote arrived in time.
	Reason string
}

// TakeBestQuote waits up to wait for a live quote on rfqID and accepts it at
// its exact amount. When the best moves before the accept lands, it takes
// the new best if one is live, or waits for the next. Any other refusal of
// the accept comes back as a closed outcome with the exchange's reason.
// onQuote sees every best quote as it arrives, including the empty one sent
// when quotes are withdrawn.
func TakeBestQuote(ctx context.Context, player QuotePlayer, rfqID string, wait time.Duration, onQuote func(stx.ComboBestQuote)) (QuoteOutcome, error) {
	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	for {
		var ev stx.ComboEvent
		select {
		case <-ctx.Done():
			return QuoteOutcome{}, nil
		case e, ok := <-player.Events():
			if !ok {
				return QuoteOutcome{}, errors.New("the combo connection closed")
			}
			ev = e
		}
		if ev.RFQID != rfqID {
			continue
		}
		switch d := ev.Data.(type) {
		case *stx.ComboRequestClosedEvent:
			return QuoteOutcome{Reason: d.Reason}, nil
		case *stx.ComboBestQuoteEvent:
			best := d.ComboBestQuote
			if onQuote != nil {
				onQuote(best)
			}
			for best.QuoteID != "" && best.Amount != "" && ctx.Err() == nil {
				placement, err := player.Accept(ctx, rfqID, best.QuoteID, best.Amount)
				if err == nil {
					return QuoteOutcome{Placed: true, Amount: best.Amount, Placement: placement}, nil
				}
				var refused *stx.ComboRefusedError
				if !errors.As(err, &refused) {
					return QuoteOutcome{}, err
				}
				if errors.Is(err, stx.ErrComboPriceMoved) && refused.Best != nil {
					// The request stays open: take the live best, or wait for the next quote.
					best = *refused.Best
					if onQuote != nil {
						onQuote(best)
					}
					continue
				}
				// Any other refusal, e.g. player_refused:nothing_to_sell when
				// the holding changed under a cash out.
				reason := refused.Code
				if refused.Detail != "" {
					reason = fmt.Sprintf("%s (%s)", refused.Code, refused.Detail)
				}
				return QuoteOutcome{Reason: reason}, nil
			}
		}
	}
}

// CashOuter is what CashOutWhenHeld needs of a [stx.ComboPlayer].
type CashOuter interface {
	CashOut(ctx context.Context, marketID string) (stx.ComboCashOut, error)
}

// CashOutWhenHeld opens a cash out of everything you hold of the combo
// marketID. A fill counts toward your position a moment after it trades, so
// straight after buying the exchange can answer nothing_to_sell: this tries
// again, up to attempts times, delay apart.
func CashOutWhenHeld(ctx context.Context, player CashOuter, marketID string, attempts int, delay time.Duration) (stx.ComboCashOut, error) {
	for attempt := 1; ; attempt++ {
		out, err := player.CashOut(ctx, marketID)
		var refused *stx.ComboRefusedError
		notYet := errors.As(err, &refused) && (refused.Code == "nothing_to_sell" || refused.Detail == "nothing_to_sell")
		if err == nil || !notYet || attempt >= attempts {
			return out, err
		}
		select {
		case <-ctx.Done():
			return out, ctx.Err()
		case <-time.After(delay):
		}
	}
}
