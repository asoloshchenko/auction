package service

import (
	"context"
	"testing"
	"time"

	"github.com/asoloshchenko/auction/internal/model"
)

func TestWorker_TickClosesExpiredAndNotifiesWinner(t *testing.T) {
	svc, db, n := newTestService()
	id := db.seed(model.Auction{
		Owner: 1, StartPrice: 100, Step: 10, EndDate: time.Now().Add(-time.Second),
		Bets: []model.Bet{{Owner: 2, Price: 150}},
	})

	NewWorker(svc, time.Hour).tick(context.Background())

	a, _ := db.GetAuction(context.Background(), id)
	if a.State != model.AuctionStateEnded {
		t.Fatalf("expected ENDED, got %s", a.State)
	}
	if n.won != 1 || n.lastWonUser != 2 {
		t.Fatalf("expected won notification to user 2, got count=%d user=%d", n.won, n.lastWonUser)
	}
}

func TestWorker_TickFiresEndingSoonOnce(t *testing.T) {
	svc, db, n := newTestService()
	db.seed(model.Auction{Owner: 1, StartPrice: 100, Step: 10, EndDate: time.Now().Add(2 * time.Minute)})

	w := NewWorker(svc, time.Hour)
	w.tick(context.Background())
	w.tick(context.Background())

	if n.endingSoon != 1 {
		t.Fatalf("ending soon must fire exactly once across ticks, got %d", n.endingSoon)
	}
}

func TestWorker_RunStopsOnContextCancel(t *testing.T) {
	svc, _, _ := newTestService()
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		NewWorker(svc, time.Millisecond).Run(ctx)
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not return after context cancel")
	}
}
