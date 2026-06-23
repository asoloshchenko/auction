package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/asoloshchenko/auction/internal/model"
	"github.com/asoloshchenko/auction/internal/oas"
	"github.com/google/uuid"
	"github.com/ogen-go/ogen/ogenerrors"
)

const defaultLimit = 20

var errUnauthorized = errors.New("authentication required")

type AuthService interface {
	Register(ctx context.Context, email, password string) (string, error)
	Login(ctx context.Context, email, password string) (string, error)
}

type AuctionService interface {
	CreateAuction(ctx context.Context, dto model.CreateAuctionDTO) (uuid.UUID, error)
	UpdateAuction(ctx context.Context, id uuid.UUID, ownerID int64, dto model.UpdateAuctionDTO) error
	CancelAuction(ctx context.Context, id uuid.UUID, ownerID int64) error
	MarkSold(ctx context.Context, id uuid.UUID, ownerID int64) error
	GetAuction(ctx context.Context, id uuid.UUID) (model.Auction, error)
	ListAuctions(ctx context.Context, limit, offset int) ([]model.Auction, error)
}

type TokenParser interface {
	Parse(token string) (int64, error)
}

func NewServer(authSvc AuthService, auctionSvc AuctionService, tokens TokenParser) (http.Handler, error) {
	handler := &Handler{auth: authSvc, auctions: auctionSvc}
	return oas.NewServer(handler, securityHandler{tokens: tokens}, oas.WithErrorHandler(errorHandler))
}

func errorHandler(_ context.Context, w http.ResponseWriter, _ *http.Request, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(ogenerrors.ErrorCode(err))
	_ = json.NewEncoder(w).Encode(oas.Error{Message: err.Error()})
}

func toOASAuction(a model.Auction) *oas.Auction {
	out := &oas.Auction{
		ID:           a.ID,
		Name:         a.Name,
		Owner:        a.Owner,
		StartPrice:   a.StartPrice,
		Step:         a.Step,
		EndDate:      a.EndDate,
		State:        oas.AuctionState(a.State),
		CurrentPrice: currentPrice(a),
		BidCount:     int32(len(a.Bets)),
	}
	if a.BuyNowPrice != nil {
		out.BuyNowPrice = oas.NewOptInt64(*a.BuyNowPrice)
	}
	if a.Winner != nil {
		out.Winner = oas.NewOptInt64(*a.Winner)
	}
	return out
}

func currentPrice(a model.Auction) int64 {
	best := a.StartPrice
	for i := range a.Bets {
		if a.Bets[i].Price > best {
			best = a.Bets[i].Price
		}
	}
	return best
}

func optToPtr(o oas.OptInt64) *int64 {
	if v, ok := o.Get(); ok {
		return &v
	}
	return nil
}
