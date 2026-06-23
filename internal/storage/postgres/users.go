package postgres

import (
	"context"
	"errors"

	"github.com/asoloshchenko/auction/internal/auth"
	"github.com/asoloshchenko/auction/internal/model"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var _ auth.UserStore = (*Store)(nil)

func (s *Store) CreateUser(ctx context.Context, email, passwordHash string) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash) VALUES ($1, $2) RETURNING id`,
		email, passwordHash,
	).Scan(&id)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
			return 0, auth.ErrEmailTaken
		}
		return 0, err
	}
	return id, nil
}

func (s *Store) GetUserByEmail(ctx context.Context, email string) (model.User, error) {
	var u model.User
	err := s.pool.QueryRow(ctx,
		`SELECT id, email, password_hash FROM users WHERE email = $1`,
		email,
	).Scan(&u.ID, &u.Email, &u.PasswordHash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.User{}, auth.ErrUserNotFound
		}
		return model.User{}, err
	}
	return u, nil
}
