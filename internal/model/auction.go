package model

import (
	"time"

	"github.com/google/uuid"
)

type AuctionState string

const (
	AuctionStateActive   AuctionState = "ACTIVE"
	AuctionStateSold     AuctionState = "SOLD"
	AuctionStateEnded    AuctionState = "ENDED"
	AuctionStateCanceled AuctionState = "CANCELED"
)

type Auction struct {
	ID          uuid.UUID
	Name        string
	Owner       int64
	StartPrice  int64
	Step        int64
	BuyNowPrice *int64
	EndDate     time.Time
	State       AuctionState
	Winner      *int64
	Bets        []Bet

	EndingSoonNotified bool
}

type CreateAuctionDTO struct {
	Name        string
	Owner       int64
	StartPrice  int64
	Step        int64
	BuyNowPrice *int64
	EndDate     time.Time
}

type UpdateAuctionDTO struct {
	Name        string
	State       AuctionState
	StartPrice  int64
	Step        int64
	BuyNowPrice *int64
	EndDate     time.Time
}

type Bet struct {
	ID        uuid.UUID
	Owner     int64
	AuctionID uuid.UUID
	Price     int64
}

type CreateBetDTO struct {
	Owner     int64
	AuctionID uuid.UUID
	Price     int64
}
