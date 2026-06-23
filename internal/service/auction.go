package service

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/asoloshchenko/auction/internal/model"
	"github.com/google/uuid"
)

const endingSoonWindow = 5 * time.Minute

var (
	ErrAuctionNotFound = errors.New("auction not found")
	ErrNotOwner        = errors.New("only the owner may modify this auction")
	ErrAuctionHasBids  = errors.New("auction already has bids")
	ErrAuctionClosed   = errors.New("auction is not open for bids")
	ErrAuctionExpired  = errors.New("auction end time has passed")
	ErrSelfBid         = errors.New("owner cannot bid on their own auction")
	ErrBidTooLow       = errors.New("bid does not meet the minimum required amount")
	ErrAuctionNotEnded = errors.New("auction is not ended with a winner")
)

type Database interface {
	CreateAuction(ctx context.Context, auction model.CreateAuctionDTO) (uuid.UUID, error)
	UpdateAuction(ctx context.Context, ID uuid.UUID, auction model.Auction) error
	DeleteAuction(ctx context.Context, ID uuid.UUID) error
	GetAuction(ctx context.Context, ID uuid.UUID) (model.Auction, error)

	ListExpiredActive(ctx context.Context, asOf time.Time) ([]model.Auction, error)
	ListEndingSoon(ctx context.Context, until time.Time) ([]model.Auction, error)
	ListAuctions(ctx context.Context, limit, offset int) ([]model.Auction, error)

	MakeBet(ctx context.Context, bet model.CreateBetDTO) (uuid.UUID, error)
}

type Notifier interface {
	BidPlaced(ctx context.Context, auctionID uuid.UUID, price int64, bidderID int64) error
	BidAccepted(ctx context.Context, userID int64, auctionID uuid.UUID, price int64) error
	Outbid(ctx context.Context, userID int64, auctionID uuid.UUID, newPrice int64) error
	AuctionWon(ctx context.Context, userID int64, auctionID uuid.UUID, finalPrice int64) error
	AuctionEndedNoBids(ctx context.Context, ownerID int64, auctionID uuid.UUID) error
	AuctionEndingSoon(ctx context.Context, auctionID uuid.UUID) error
}

type Service struct {
	db       Database
	notifier Notifier

	mu    sync.Mutex
	locks map[uuid.UUID]*sync.Mutex
}

func NewService(db Database, notifier Notifier) *Service {
	return &Service{
		db:       db,
		notifier: notifier,
		locks:    make(map[uuid.UUID]*sync.Mutex),
	}
}

func (s *Service) auctionLock(id uuid.UUID) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	lock, ok := s.locks[id]
	if !ok {
		lock = &sync.Mutex{}
		s.locks[id] = lock
	}
	return lock
}

func (s *Service) dropLock(id uuid.UUID) {
	s.mu.Lock()
	delete(s.locks, id)
	s.mu.Unlock()
}

func (s *Service) CreateAuction(ctx context.Context, dto model.CreateAuctionDTO) (uuid.UUID, error) {
	id, err := s.db.CreateAuction(ctx, dto)
	if err != nil {
		return uuid.Nil, err
	}
	slog.InfoContext(ctx, "auction created", "auction_id", id, "owner", dto.Owner, "start_price", dto.StartPrice, "end_date", dto.EndDate)
	return id, nil
}

func (s *Service) UpdateAuction(ctx context.Context, id uuid.UUID, ownerID int64, dto model.UpdateAuctionDTO) error {
	lock := s.auctionLock(id)
	lock.Lock()
	defer lock.Unlock()

	auction, err := s.db.GetAuction(ctx, id)
	if err != nil {
		return ErrAuctionNotFound
	}
	if auction.Owner != ownerID {
		return ErrNotOwner
	}
	if len(auction.Bets) > 0 {
		return ErrAuctionHasBids
	}

	auction.Name = dto.Name
	auction.StartPrice = dto.StartPrice
	auction.Step = dto.Step
	auction.BuyNowPrice = dto.BuyNowPrice
	auction.EndDate = dto.EndDate
	return s.db.UpdateAuction(ctx, id, auction)
}

func (s *Service) CancelAuction(ctx context.Context, id uuid.UUID, ownerID int64) error {
	lock := s.auctionLock(id)
	lock.Lock()
	defer lock.Unlock()

	auction, err := s.db.GetAuction(ctx, id)
	if err != nil {
		return ErrAuctionNotFound
	}
	if auction.Owner != ownerID {
		return ErrNotOwner
	}
	if len(auction.Bets) > 0 {
		return ErrAuctionHasBids
	}

	auction.State = model.AuctionStateCanceled
	if err := s.db.UpdateAuction(ctx, id, auction); err != nil {
		return err
	}
	s.dropLock(id)
	slog.InfoContext(ctx, "auction canceled", "auction_id", id, "owner", ownerID)
	return nil
}

func (s *Service) MarkSold(ctx context.Context, id uuid.UUID, ownerID int64) error {
	lock := s.auctionLock(id)
	lock.Lock()
	defer lock.Unlock()

	auction, err := s.db.GetAuction(ctx, id)
	if err != nil {
		return ErrAuctionNotFound
	}
	if auction.Owner != ownerID {
		return ErrNotOwner
	}
	if auction.State != model.AuctionStateEnded || auction.Winner == nil {
		return ErrAuctionNotEnded
	}

	auction.State = model.AuctionStateSold
	if err := s.db.UpdateAuction(ctx, id, auction); err != nil {
		return err
	}
	s.dropLock(id)
	slog.InfoContext(ctx, "auction marked sold", "auction_id", id, "owner", ownerID, "winner", *auction.Winner)
	return nil
}

func (s *Service) GetAuction(ctx context.Context, id uuid.UUID) (model.Auction, error) {
	auction, err := s.db.GetAuction(ctx, id)
	if err != nil {
		return model.Auction{}, ErrAuctionNotFound
	}
	return auction, nil
}

func (s *Service) ListAuctions(ctx context.Context, limit, offset int) ([]model.Auction, error) {
	return s.db.ListAuctions(ctx, limit, offset)
}

func (s *Service) PlaceBet(ctx context.Context, dto model.CreateBetDTO) (betID uuid.UUID, won bool, err error) {
	lock := s.auctionLock(dto.AuctionID)
	lock.Lock()
	defer lock.Unlock()

	auction, err := s.db.GetAuction(ctx, dto.AuctionID)
	if err != nil {
		return uuid.Nil, false, ErrAuctionNotFound
	}
	if auction.State != model.AuctionStateActive {
		return uuid.Nil, false, ErrAuctionClosed
	}
	if !time.Now().Before(auction.EndDate) {
		return uuid.Nil, false, ErrAuctionExpired
	}
	if dto.Owner == auction.Owner {
		return uuid.Nil, false, ErrSelfBid
	}
	if dto.Price < minBid(auction) {
		return uuid.Nil, false, ErrBidTooLow
	}

	prev := highestBet(auction)

	betID, err = s.db.MakeBet(ctx, dto)
	if err != nil {
		return uuid.Nil, false, err
	}
	auction.Bets = append(auction.Bets, model.Bet{
		ID:        betID,
		Owner:     dto.Owner,
		AuctionID: dto.AuctionID,
		Price:     dto.Price,
	})

	if prev != nil && prev.Owner != dto.Owner {
		_ = s.notifier.Outbid(ctx, prev.Owner, auction.ID, dto.Price)
	}
	_ = s.notifier.BidAccepted(ctx, dto.Owner, auction.ID, dto.Price)
	_ = s.notifier.BidPlaced(ctx, auction.ID, dto.Price, dto.Owner)
	slog.InfoContext(ctx, "bid accepted", "auction_id", auction.ID, "bidder", dto.Owner, "price", dto.Price)

	if auction.BuyNowPrice != nil && dto.Price >= *auction.BuyNowPrice {
		auction.State = model.AuctionStateEnded
		auction.Winner = &dto.Owner
		if err := s.db.UpdateAuction(ctx, auction.ID, auction); err != nil {
			return betID, false, err
		}
		s.dropLock(auction.ID)
		_ = s.notifier.AuctionWon(ctx, dto.Owner, auction.ID, dto.Price)
		slog.InfoContext(ctx, "auction ended via buy now", "auction_id", auction.ID, "winner", dto.Owner, "price", dto.Price)
		return betID, true, nil
	}

	return betID, false, nil
}

func (s *Service) CloseExpiredAuctions(ctx context.Context) error {
	now := time.Now()
	expired, err := s.db.ListExpiredActive(ctx, now)
	if err != nil {
		return err
	}

	var errs []error
	for _, a := range expired {
		if err := s.closeOne(ctx, a.ID); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (s *Service) closeOne(ctx context.Context, id uuid.UUID) error {
	lock := s.auctionLock(id)
	lock.Lock()
	defer lock.Unlock()

	auction, err := s.db.GetAuction(ctx, id)
	if err != nil {
		return err
	}
	if auction.State != model.AuctionStateActive || time.Now().Before(auction.EndDate) {
		return nil
	}

	auction.State = model.AuctionStateEnded
	winner := highestBet(auction)
	if winner == nil {
		if err := s.db.UpdateAuction(ctx, id, auction); err != nil {
			return err
		}
		s.dropLock(id)
		_ = s.notifier.AuctionEndedNoBids(ctx, auction.Owner, id)
		slog.InfoContext(ctx, "auction ended without bids", "auction_id", id, "owner", auction.Owner)
		return nil
	}

	auction.Winner = &winner.Owner
	if err := s.db.UpdateAuction(ctx, id, auction); err != nil {
		return err
	}
	s.dropLock(id)
	_ = s.notifier.AuctionWon(ctx, winner.Owner, id, winner.Price)
	slog.InfoContext(ctx, "auction ended", "auction_id", id, "winner", winner.Owner, "price", winner.Price)
	return nil
}

func (s *Service) NotifyEndingSoon(ctx context.Context) error {
	until := time.Now().Add(endingSoonWindow)
	soon, err := s.db.ListEndingSoon(ctx, until)
	if err != nil {
		return err
	}

	var errs []error
	for _, a := range soon {
		lock := s.auctionLock(a.ID)
		lock.Lock()
		auction, err := s.db.GetAuction(ctx, a.ID)
		if err != nil {
			lock.Unlock()
			errs = append(errs, err)
			continue
		}
		if auction.State != model.AuctionStateActive || auction.EndingSoonNotified {
			lock.Unlock()
			continue
		}
		auction.EndingSoonNotified = true
		if err := s.db.UpdateAuction(ctx, auction.ID, auction); err != nil {
			lock.Unlock()
			errs = append(errs, err)
			continue
		}
		lock.Unlock()
		_ = s.notifier.AuctionEndingSoon(ctx, auction.ID)
		slog.InfoContext(ctx, "auction ending soon", "auction_id", auction.ID, "end_date", auction.EndDate)
	}
	return errors.Join(errs...)
}

func minBid(a model.Auction) int64 {
	if h := highestBet(a); h != nil {
		return h.Price + a.Step
	}
	return a.StartPrice
}

func highestBet(a model.Auction) *model.Bet {
	var best *model.Bet
	for i := range a.Bets {
		if best == nil || a.Bets[i].Price > best.Price {
			best = &a.Bets[i]
		}
	}
	return best
}
