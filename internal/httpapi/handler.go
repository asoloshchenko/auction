package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/asoloshchenko/auction/internal/auth"
	"github.com/asoloshchenko/auction/internal/model"
	"github.com/asoloshchenko/auction/internal/oas"
	"github.com/asoloshchenko/auction/internal/service"
	"github.com/ogen-go/ogen/ogenerrors"
)

type Handler struct {
	auth     AuthService
	auctions AuctionService
}

var _ oas.Handler = (*Handler)(nil)

func (h *Handler) RegisterUser(ctx context.Context, req *oas.Credentials) (*oas.TokenResponse, error) {
	token, err := h.auth.Register(ctx, req.Email, req.Password)
	if err != nil {
		return nil, err
	}
	return &oas.TokenResponse{Token: token}, nil
}

func (h *Handler) LoginUser(ctx context.Context, req *oas.Credentials) (*oas.TokenResponse, error) {
	token, err := h.auth.Login(ctx, req.Email, req.Password)
	if err != nil {
		return nil, err
	}
	return &oas.TokenResponse{Token: token}, nil
}

func (h *Handler) CreateAuction(ctx context.Context, req *oas.CreateAuctionRequest) (*oas.Auction, error) {
	userID, ok := auth.UserIDFromContext(ctx)
	if !ok {
		return nil, errUnauthorized
	}
	id, err := h.auctions.CreateAuction(ctx, model.CreateAuctionDTO{
		Name:        req.Name,
		Owner:       userID,
		StartPrice:  req.StartPrice,
		Step:        req.Step,
		BuyNowPrice: optToPtr(req.BuyNowPrice),
		EndDate:     req.EndDate,
	})
	if err != nil {
		return nil, err
	}
	a, err := h.auctions.GetAuction(ctx, id)
	if err != nil {
		return nil, err
	}
	return toOASAuction(a), nil
}

func (h *Handler) ListAuctions(ctx context.Context, params oas.ListAuctionsParams) (*oas.AuctionList, error) {
	limit := int(params.Limit.Or(defaultLimit))
	offset := int(params.Offset.Or(0))

	list, err := h.auctions.ListAuctions(ctx, limit, offset)
	if err != nil {
		return nil, err
	}
	items := make([]oas.Auction, len(list))
	for i := range list {
		items[i] = *toOASAuction(list[i])
	}
	return &oas.AuctionList{Items: items, Limit: int32(limit), Offset: int32(offset)}, nil
}

func (h *Handler) GetAuction(ctx context.Context, params oas.GetAuctionParams) (*oas.Auction, error) {
	a, err := h.auctions.GetAuction(ctx, params.ID)
	if err != nil {
		return nil, err
	}
	return toOASAuction(a), nil
}

func (h *Handler) UpdateAuction(ctx context.Context, req *oas.UpdateAuctionRequest, params oas.UpdateAuctionParams) (*oas.Auction, error) {
	userID, ok := auth.UserIDFromContext(ctx)
	if !ok {
		return nil, errUnauthorized
	}
	err := h.auctions.UpdateAuction(ctx, params.ID, userID, model.UpdateAuctionDTO{
		Name:        req.Name,
		StartPrice:  req.StartPrice,
		Step:        req.Step,
		BuyNowPrice: optToPtr(req.BuyNowPrice),
		EndDate:     req.EndDate,
	})
	if err != nil {
		return nil, err
	}
	a, err := h.auctions.GetAuction(ctx, params.ID)
	if err != nil {
		return nil, err
	}
	return toOASAuction(a), nil
}

func (h *Handler) CancelAuction(ctx context.Context, params oas.CancelAuctionParams) (*oas.Auction, error) {
	userID, ok := auth.UserIDFromContext(ctx)
	if !ok {
		return nil, errUnauthorized
	}
	if err := h.auctions.CancelAuction(ctx, params.ID, userID); err != nil {
		return nil, err
	}
	a, err := h.auctions.GetAuction(ctx, params.ID)
	if err != nil {
		return nil, err
	}
	return toOASAuction(a), nil
}

func (h *Handler) MarkAuctionSold(ctx context.Context, params oas.MarkAuctionSoldParams) (*oas.Auction, error) {
	userID, ok := auth.UserIDFromContext(ctx)
	if !ok {
		return nil, errUnauthorized
	}
	if err := h.auctions.MarkSold(ctx, params.ID, userID); err != nil {
		return nil, err
	}
	a, err := h.auctions.GetAuction(ctx, params.ID)
	if err != nil {
		return nil, err
	}
	return toOASAuction(a), nil
}

func (h *Handler) NewError(_ context.Context, err error) *oas.ErrorStatusCode {
	code, msg := http.StatusInternalServerError, "internal error"
	var secErr *ogenerrors.SecurityError
	switch {
	case errors.As(err, &secErr):
		code, msg = http.StatusUnauthorized, "unauthorized"
	case errors.Is(err, auth.ErrInvalidInput):
		code, msg = http.StatusBadRequest, err.Error()
	case errors.Is(err, auth.ErrEmailTaken):
		code, msg = http.StatusConflict, err.Error()
	case errors.Is(err, auth.ErrInvalidCredentials):
		code, msg = http.StatusUnauthorized, err.Error()
	case errors.Is(err, errUnauthorized):
		code, msg = http.StatusUnauthorized, err.Error()
	case errors.Is(err, service.ErrAuctionNotFound):
		code, msg = http.StatusNotFound, err.Error()
	case errors.Is(err, service.ErrNotOwner), errors.Is(err, service.ErrSelfBid):
		code, msg = http.StatusForbidden, err.Error()
	case errors.Is(err, service.ErrAuctionHasBids),
		errors.Is(err, service.ErrAuctionClosed),
		errors.Is(err, service.ErrAuctionExpired),
		errors.Is(err, service.ErrAuctionNotEnded),
		errors.Is(err, service.ErrBidTooLow):
		code, msg = http.StatusConflict, err.Error()
	}
	return &oas.ErrorStatusCode{StatusCode: code, Response: oas.Error{Message: msg}}
}
