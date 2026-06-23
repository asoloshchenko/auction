package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/asoloshchenko/auction/internal/model"
	"github.com/asoloshchenko/auction/internal/service"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var _ service.Database = (*Store)(nil)

const pgUniqueViolation = "23505"

type Store struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

const auctionColumns = `id, name, owner, start_price, step, buy_now_price, end_date, state, winner, ending_soon_notified`

func scanAuction(row pgx.Row) (model.Auction, error) {
	var (
		a     model.Auction
		idStr string
		state string
	)
	err := row.Scan(&idStr, &a.Name, &a.Owner, &a.StartPrice, &a.Step, &a.BuyNowPrice, &a.EndDate, &state, &a.Winner, &a.EndingSoonNotified)
	if err != nil {
		return model.Auction{}, err
	}
	a.ID, err = uuid.Parse(idStr)
	if err != nil {
		return model.Auction{}, err
	}
	a.State = model.AuctionState(state)
	return a, nil
}

func (s *Store) CreateAuction(ctx context.Context, dto model.CreateAuctionDTO) (uuid.UUID, error) {
	id := uuid.New()
	_, err := s.pool.Exec(ctx,
		`INSERT INTO auctions (id, name, owner, start_price, step, buy_now_price, end_date, state, ending_soon_notified)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, false)`,
		id.String(), dto.Name, dto.Owner, dto.StartPrice, dto.Step, dto.BuyNowPrice, dto.EndDate, string(model.AuctionStateActive),
	)
	if err != nil {
		return uuid.Nil, err
	}
	return id, nil
}

func (s *Store) UpdateAuction(ctx context.Context, id uuid.UUID, a model.Auction) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE auctions
		 SET name = $2, start_price = $3, step = $4, buy_now_price = $5,
		     end_date = $6, state = $7, winner = $8, ending_soon_notified = $9
		 WHERE id = $1`,
		id.String(), a.Name, a.StartPrice, a.Step, a.BuyNowPrice, a.EndDate, string(a.State), a.Winner, a.EndingSoonNotified,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return service.ErrAuctionNotFound
	}
	return nil
}

func (s *Store) DeleteAuction(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM auctions WHERE id = $1`, id.String())
	return err
}

func (s *Store) GetAuction(ctx context.Context, id uuid.UUID) (model.Auction, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+auctionColumns+` FROM auctions WHERE id = $1`, id.String())
	a, err := scanAuction(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Auction{}, service.ErrAuctionNotFound
		}
		return model.Auction{}, err
	}

	bets, err := s.betsByAuction(ctx, id)
	if err != nil {
		return model.Auction{}, err
	}
	a.Bets = bets
	return a, nil
}

func (s *Store) betsByAuction(ctx context.Context, auctionID uuid.UUID) ([]model.Bet, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, owner, auction_id, price FROM bets WHERE auction_id = $1 ORDER BY price`,
		auctionID.String(),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var bets []model.Bet
	for rows.Next() {
		var (
			b      model.Bet
			idStr  string
			aidStr string
		)
		if err := rows.Scan(&idStr, &b.Owner, &aidStr, &b.Price); err != nil {
			return nil, err
		}
		if b.ID, err = uuid.Parse(idStr); err != nil {
			return nil, err
		}
		if b.AuctionID, err = uuid.Parse(aidStr); err != nil {
			return nil, err
		}
		bets = append(bets, b)
	}
	return bets, rows.Err()
}

func (s *Store) ListExpiredActive(ctx context.Context, asOf time.Time) ([]model.Auction, error) {
	return s.listAuctions(ctx,
		`SELECT `+auctionColumns+` FROM auctions WHERE state = $1 AND end_date <= $2`,
		string(model.AuctionStateActive), asOf,
	)
}

func (s *Store) ListEndingSoon(ctx context.Context, until time.Time) ([]model.Auction, error) {
	return s.listAuctions(ctx,
		`SELECT `+auctionColumns+` FROM auctions WHERE state = $1 AND ending_soon_notified = false AND end_date <= $2`,
		string(model.AuctionStateActive), until,
	)
}

func (s *Store) ListAuctions(ctx context.Context, limit, offset int) ([]model.Auction, error) {
	auctions, err := s.listAuctions(ctx,
		`SELECT `+auctionColumns+` FROM auctions ORDER BY created_at DESC LIMIT $1 OFFSET $2`,
		limit, offset,
	)
	if err != nil || len(auctions) == 0 {
		return auctions, err
	}

	ids := make([]string, len(auctions))
	for i := range auctions {
		ids[i] = auctions[i].ID.String()
	}

	rows, err := s.pool.Query(ctx,
		`SELECT id, owner, auction_id, price FROM bets WHERE auction_id::text = ANY($1) ORDER BY price`,
		ids,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	betsByID := make(map[uuid.UUID][]model.Bet, len(auctions))
	for rows.Next() {
		var (
			b      model.Bet
			idStr  string
			aidStr string
		)
		if err := rows.Scan(&idStr, &b.Owner, &aidStr, &b.Price); err != nil {
			return nil, err
		}
		if b.ID, err = uuid.Parse(idStr); err != nil {
			return nil, err
		}
		if b.AuctionID, err = uuid.Parse(aidStr); err != nil {
			return nil, err
		}
		betsByID[b.AuctionID] = append(betsByID[b.AuctionID], b)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i := range auctions {
		auctions[i].Bets = betsByID[auctions[i].ID]
	}
	return auctions, nil
}

func (s *Store) listAuctions(ctx context.Context, query string, args ...any) ([]model.Auction, error) {
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var auctions []model.Auction
	for rows.Next() {
		a, err := scanAuction(rows)
		if err != nil {
			return nil, err
		}
		auctions = append(auctions, a)
	}
	return auctions, rows.Err()
}

func (s *Store) MakeBet(ctx context.Context, bet model.CreateBetDTO) (uuid.UUID, error) {
	id := uuid.New()
	_, err := s.pool.Exec(ctx,
		`INSERT INTO bets (id, auction_id, owner, price) VALUES ($1, $2, $3, $4)`,
		id.String(), bet.AuctionID.String(), bet.Owner, bet.Price,
	)
	if err != nil {
		return uuid.Nil, err
	}
	return id, nil
}
