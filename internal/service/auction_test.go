package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/asoloshchenko/auction/internal/model"
	"github.com/google/uuid"
)

type fakeDB struct {
	mu       sync.Mutex
	auctions map[uuid.UUID]model.Auction
}

func newFakeDB() *fakeDB {
	return &fakeDB{auctions: make(map[uuid.UUID]model.Auction)}
}

func cloneBets(bets []model.Bet) []model.Bet {
	if bets == nil {
		return nil
	}
	out := make([]model.Bet, len(bets))
	copy(out, bets)
	return out
}

func (f *fakeDB) seed(a model.Auction) uuid.UUID {
	f.mu.Lock()
	defer f.mu.Unlock()
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	if a.State == "" {
		a.State = model.AuctionStateActive
	}
	f.auctions[a.ID] = a
	return a.ID
}

func (f *fakeDB) CreateAuction(ctx context.Context, dto model.CreateAuctionDTO) (uuid.UUID, error) {
	return f.seed(model.Auction{
		Name:        dto.Name,
		Owner:       dto.Owner,
		StartPrice:  dto.StartPrice,
		Step:        dto.Step,
		BuyNowPrice: dto.BuyNowPrice,
		EndDate:     dto.EndDate,
		State:       model.AuctionStateActive,
	}), nil
}

func (f *fakeDB) UpdateAuction(ctx context.Context, id uuid.UUID, a model.Auction) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.auctions[id]; !ok {
		return ErrAuctionNotFound
	}
	a.Bets = cloneBets(a.Bets)
	f.auctions[id] = a
	return nil
}

func (f *fakeDB) DeleteAuction(ctx context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.auctions, id)
	return nil
}

func (f *fakeDB) GetAuction(ctx context.Context, id uuid.UUID) (model.Auction, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.auctions[id]
	if !ok {
		return model.Auction{}, ErrAuctionNotFound
	}
	a.Bets = cloneBets(a.Bets)
	return a, nil
}

func (f *fakeDB) ListExpiredActive(ctx context.Context, asOf time.Time) ([]model.Auction, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []model.Auction
	for _, a := range f.auctions {
		if a.State == model.AuctionStateActive && !a.EndDate.After(asOf) {
			a.Bets = cloneBets(a.Bets)
			out = append(out, a)
		}
	}
	return out, nil
}

func (f *fakeDB) ListEndingSoon(ctx context.Context, until time.Time) ([]model.Auction, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []model.Auction
	for _, a := range f.auctions {
		if a.State == model.AuctionStateActive && !a.EndingSoonNotified && !a.EndDate.After(until) {
			a.Bets = cloneBets(a.Bets)
			out = append(out, a)
		}
	}
	return out, nil
}

func (f *fakeDB) ListAuctions(ctx context.Context, limit, offset int) ([]model.Auction, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []model.Auction
	for _, a := range f.auctions {
		a.Bets = cloneBets(a.Bets)
		out = append(out, a)
	}
	if offset >= len(out) {
		return nil, nil
	}
	end := offset + limit
	if end > len(out) {
		end = len(out)
	}
	return out[offset:end], nil
}

func (f *fakeDB) MakeBet(ctx context.Context, bet model.CreateBetDTO) (uuid.UUID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.auctions[bet.AuctionID]
	if !ok {
		return uuid.Nil, ErrAuctionNotFound
	}
	id := uuid.New()
	a.Bets = append(a.Bets, model.Bet{ID: id, Owner: bet.Owner, AuctionID: bet.AuctionID, Price: bet.Price})
	f.auctions[bet.AuctionID] = a
	return id, nil
}

type recordingNotifier struct {
	mu                 sync.Mutex
	bidPlaced          int
	bidAccepted        int
	outbid             int
	won                int
	endedNoBids        int
	endingSoon         int
	lastWonUser        int64
	lastWonPrice       int64
	lastEndedNoBidsOwn int64
}

func (n *recordingNotifier) BidPlaced(ctx context.Context, auctionID uuid.UUID, price, bidderID int64) error {
	n.mu.Lock()
	n.bidPlaced++
	n.mu.Unlock()
	return nil
}

func (n *recordingNotifier) BidAccepted(ctx context.Context, userID int64, auctionID uuid.UUID, price int64) error {
	n.mu.Lock()
	n.bidAccepted++
	n.mu.Unlock()
	return nil
}

func (n *recordingNotifier) Outbid(ctx context.Context, userID int64, auctionID uuid.UUID, newPrice int64) error {
	n.mu.Lock()
	n.outbid++
	n.mu.Unlock()
	return nil
}

func (n *recordingNotifier) AuctionWon(ctx context.Context, userID int64, auctionID uuid.UUID, finalPrice int64) error {
	n.mu.Lock()
	n.won++
	n.lastWonUser = userID
	n.lastWonPrice = finalPrice
	n.mu.Unlock()
	return nil
}

func (n *recordingNotifier) AuctionEndedNoBids(ctx context.Context, ownerID int64, auctionID uuid.UUID) error {
	n.mu.Lock()
	n.endedNoBids++
	n.lastEndedNoBidsOwn = ownerID
	n.mu.Unlock()
	return nil
}

func (n *recordingNotifier) AuctionEndingSoon(ctx context.Context, auctionID uuid.UUID) error {
	n.mu.Lock()
	n.endingSoon++
	n.mu.Unlock()
	return nil
}

func ptr(v int64) *int64 { return &v }

func newTestService() (*Service, *fakeDB, *recordingNotifier) {
	db := newFakeDB()
	n := &recordingNotifier{}
	return NewService(db, n), db, n
}

func TestPlaceBet_ValidFirstBid(t *testing.T) {
	svc, db, n := newTestService()
	id := db.seed(model.Auction{Owner: 1, StartPrice: 100, Step: 10, EndDate: time.Now().Add(time.Hour)})

	_, won, err := svc.PlaceBet(context.Background(), model.CreateBetDTO{Owner: 2, AuctionID: id, Price: 100})
	if err != nil {
		t.Fatalf("expected bid to be accepted, got %v", err)
	}
	if won {
		t.Fatal("first bid should not win without buy now")
	}
	if n.bidAccepted != 1 || n.bidPlaced != 1 {
		t.Fatalf("expected accepted+placed notifications, got accepted=%d placed=%d", n.bidAccepted, n.bidPlaced)
	}
}

func TestPlaceBet_BelowMinStep(t *testing.T) {
	svc, db, _ := newTestService()
	id := db.seed(model.Auction{
		Owner: 1, StartPrice: 100, Step: 10, EndDate: time.Now().Add(time.Hour),
		Bets: []model.Bet{{Owner: 2, Price: 150}},
	})

	_, _, err := svc.PlaceBet(context.Background(), model.CreateBetDTO{Owner: 3, AuctionID: id, Price: 155})
	if !errors.Is(err, ErrBidTooLow) {
		t.Fatalf("expected ErrBidTooLow, got %v", err)
	}
}

func TestPlaceBet_FirstBidBelowStartPrice(t *testing.T) {
	svc, db, _ := newTestService()
	id := db.seed(model.Auction{Owner: 1, StartPrice: 100, Step: 10, EndDate: time.Now().Add(time.Hour)})

	_, _, err := svc.PlaceBet(context.Background(), model.CreateBetDTO{Owner: 2, AuctionID: id, Price: 99})
	if !errors.Is(err, ErrBidTooLow) {
		t.Fatalf("expected ErrBidTooLow, got %v", err)
	}
}

func TestPlaceBet_OwnerCannotBid(t *testing.T) {
	svc, db, _ := newTestService()
	id := db.seed(model.Auction{Owner: 1, StartPrice: 100, Step: 10, EndDate: time.Now().Add(time.Hour)})

	_, _, err := svc.PlaceBet(context.Background(), model.CreateBetDTO{Owner: 1, AuctionID: id, Price: 200})
	if !errors.Is(err, ErrSelfBid) {
		t.Fatalf("expected ErrSelfBid, got %v", err)
	}
}

func TestPlaceBet_OnEndedAuctionByState(t *testing.T) {
	svc, db, _ := newTestService()
	id := db.seed(model.Auction{Owner: 1, StartPrice: 100, Step: 10, State: model.AuctionStateEnded, EndDate: time.Now().Add(time.Hour)})

	_, _, err := svc.PlaceBet(context.Background(), model.CreateBetDTO{Owner: 2, AuctionID: id, Price: 200})
	if !errors.Is(err, ErrAuctionClosed) {
		t.Fatalf("expected ErrAuctionClosed, got %v", err)
	}
}

func TestPlaceBet_OnExpiredAuctionByTime(t *testing.T) {
	svc, db, _ := newTestService()
	id := db.seed(model.Auction{Owner: 1, StartPrice: 100, Step: 10, EndDate: time.Now().Add(-time.Minute)})

	_, _, err := svc.PlaceBet(context.Background(), model.CreateBetDTO{Owner: 2, AuctionID: id, Price: 200})
	if !errors.Is(err, ErrAuctionExpired) {
		t.Fatalf("expected ErrAuctionExpired, got %v", err)
	}
}

func TestPlaceBet_BuyNowTriggersImmediateClose(t *testing.T) {
	svc, db, n := newTestService()
	id := db.seed(model.Auction{Owner: 1, StartPrice: 100, Step: 10, BuyNowPrice: ptr(500), EndDate: time.Now().Add(time.Hour)})

	_, won, err := svc.PlaceBet(context.Background(), model.CreateBetDTO{Owner: 2, AuctionID: id, Price: 500})
	if err != nil {
		t.Fatalf("expected bid accepted, got %v", err)
	}
	if !won {
		t.Fatal("buy now bid should win")
	}

	a, _ := db.GetAuction(context.Background(), id)
	if a.State != model.AuctionStateEnded {
		t.Fatalf("expected ENDED, got %s", a.State)
	}
	if a.Winner == nil || *a.Winner != 2 {
		t.Fatalf("expected winner 2, got %v", a.Winner)
	}
	if n.won != 1 || n.lastWonUser != 2 || n.lastWonPrice != 500 {
		t.Fatalf("unexpected won notification: count=%d user=%d price=%d", n.won, n.lastWonUser, n.lastWonPrice)
	}

	_, _, err = svc.PlaceBet(context.Background(), model.CreateBetDTO{Owner: 3, AuctionID: id, Price: 600})
	if !errors.Is(err, ErrAuctionClosed) {
		t.Fatalf("expected closed after buy now, got %v", err)
	}
}

func TestMarkSold_OwnerOnEndedWithWinner(t *testing.T) {
	svc, db, _ := newTestService()
	winner := int64(2)
	id := db.seed(model.Auction{
		Owner: 1, StartPrice: 100, Step: 10, State: model.AuctionStateEnded,
		Winner: &winner, EndDate: time.Now().Add(-time.Minute),
		Bets: []model.Bet{{Owner: 2, Price: 150}},
	})

	if err := svc.MarkSold(context.Background(), id, 1); err != nil {
		t.Fatalf("expected sold, got %v", err)
	}
	a, _ := db.GetAuction(context.Background(), id)
	if a.State != model.AuctionStateSold {
		t.Fatalf("expected SOLD, got %s", a.State)
	}
}

func TestMarkSold_RejectsNonOwner(t *testing.T) {
	svc, db, _ := newTestService()
	winner := int64(2)
	id := db.seed(model.Auction{Owner: 1, State: model.AuctionStateEnded, Winner: &winner})

	if err := svc.MarkSold(context.Background(), id, 99); !errors.Is(err, ErrNotOwner) {
		t.Fatalf("expected ErrNotOwner, got %v", err)
	}
}

func TestMarkSold_RejectsActiveOrNoWinner(t *testing.T) {
	svc, db, _ := newTestService()
	active := db.seed(model.Auction{Owner: 1, State: model.AuctionStateActive, EndDate: time.Now().Add(time.Hour)})
	if err := svc.MarkSold(context.Background(), active, 1); !errors.Is(err, ErrAuctionNotEnded) {
		t.Fatalf("active: expected ErrAuctionNotEnded, got %v", err)
	}

	noWinner := db.seed(model.Auction{Owner: 1, State: model.AuctionStateEnded})
	if err := svc.MarkSold(context.Background(), noWinner, 1); !errors.Is(err, ErrAuctionNotEnded) {
		t.Fatalf("no winner: expected ErrAuctionNotEnded, got %v", err)
	}
}

func TestCloseExpiredAuctions_SelectsHighestBidder(t *testing.T) {
	svc, db, n := newTestService()
	id := db.seed(model.Auction{
		Owner: 1, StartPrice: 100, Step: 10, EndDate: time.Now().Add(-time.Second),
		Bets: []model.Bet{{Owner: 2, Price: 120}, {Owner: 3, Price: 150}, {Owner: 2, Price: 140}},
	})

	if err := svc.CloseExpiredAuctions(context.Background()); err != nil {
		t.Fatalf("close failed: %v", err)
	}

	a, _ := db.GetAuction(context.Background(), id)
	if a.State != model.AuctionStateEnded {
		t.Fatalf("expected ENDED, got %s", a.State)
	}
	if a.Winner == nil || *a.Winner != 3 {
		t.Fatalf("expected winner 3, got %v", a.Winner)
	}
	if n.won != 1 || n.lastWonUser != 3 || n.lastWonPrice != 150 {
		t.Fatalf("unexpected won notification: count=%d user=%d price=%d", n.won, n.lastWonUser, n.lastWonPrice)
	}
}

func TestCloseExpiredAuctions_NoBids(t *testing.T) {
	svc, db, n := newTestService()
	id := db.seed(model.Auction{Owner: 7, StartPrice: 100, Step: 10, EndDate: time.Now().Add(-time.Second)})

	if err := svc.CloseExpiredAuctions(context.Background()); err != nil {
		t.Fatalf("close failed: %v", err)
	}

	a, _ := db.GetAuction(context.Background(), id)
	if a.State != model.AuctionStateEnded {
		t.Fatalf("expected ENDED, got %s", a.State)
	}
	if a.Winner != nil {
		t.Fatalf("expected no winner, got %v", *a.Winner)
	}
	if n.endedNoBids != 1 || n.lastEndedNoBidsOwn != 7 {
		t.Fatalf("expected no-bids notification to owner 7, got count=%d owner=%d", n.endedNoBids, n.lastEndedNoBidsOwn)
	}
	if n.won != 0 {
		t.Fatalf("expected no won notification, got %d", n.won)
	}
}

func TestCloseExpiredAuctions_LeavesActiveUntouched(t *testing.T) {
	svc, db, _ := newTestService()
	id := db.seed(model.Auction{Owner: 1, StartPrice: 100, Step: 10, EndDate: time.Now().Add(time.Hour)})

	if err := svc.CloseExpiredAuctions(context.Background()); err != nil {
		t.Fatalf("close failed: %v", err)
	}

	a, _ := db.GetAuction(context.Background(), id)
	if a.State != model.AuctionStateActive {
		t.Fatalf("expected still ACTIVE, got %s", a.State)
	}
}

func TestPlaceBet_ConcurrentBidsKeepStateConsistent(t *testing.T) {
	svc, db, _ := newTestService()
	id := db.seed(model.Auction{Owner: 1, StartPrice: 100, Step: 10, EndDate: time.Now().Add(time.Hour)})

	const n = 50
	var wg sync.WaitGroup
	var mu sync.Mutex
	accepted := 0

	start := make(chan struct{})
	for i := range n {
		wg.Add(1)
		go func(bidder int64) {
			defer wg.Done()
			<-start
			_, _, err := svc.PlaceBet(context.Background(), model.CreateBetDTO{Owner: bidder, AuctionID: id, Price: 150})
			if err == nil {
				mu.Lock()
				accepted++
				mu.Unlock()
			}
		}(int64(i + 2))
	}
	close(start)
	wg.Wait()

	if accepted != 1 {
		t.Fatalf("exactly one equal-price bid must be accepted, got %d", accepted)
	}

	a, _ := db.GetAuction(context.Background(), id)
	if len(a.Bets) != 1 {
		t.Fatalf("expected exactly one stored bet, got %d", len(a.Bets))
	}
	if h := highestBet(a); h == nil || h.Price != 150 {
		t.Fatalf("expected highest bid 150, got %v", h)
	}
}

func TestNotifyEndingSoon_FiresOnce(t *testing.T) {
	svc, db, notif := newTestService()
	id := db.seed(model.Auction{Owner: 1, StartPrice: 100, Step: 10, EndDate: time.Now().Add(2 * time.Minute)})

	if err := svc.NotifyEndingSoon(context.Background()); err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	if err := svc.NotifyEndingSoon(context.Background()); err != nil {
		t.Fatalf("second call failed: %v", err)
	}

	if notif.endingSoon != 1 {
		t.Fatalf("ending soon must fire exactly once, got %d", notif.endingSoon)
	}
	a, _ := db.GetAuction(context.Background(), id)
	if !a.EndingSoonNotified {
		t.Fatal("expected EndingSoonNotified to be persisted")
	}
}
