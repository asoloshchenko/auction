package auth

import (
	"context"
	"errors"
	"strings"

	"github.com/asoloshchenko/auction/internal/model"
)

var (
	ErrEmailTaken         = errors.New("email already registered")
	ErrUserNotFound       = errors.New("user not found")
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrInvalidInput       = errors.New("invalid email or password format")
)

const minPasswordLen = 8

type UserStore interface {
	CreateUser(ctx context.Context, email, passwordHash string) (int64, error)
	GetUserByEmail(ctx context.Context, email string) (model.User, error)
}

type Service struct {
	users  UserStore
	tokens *TokenManager
}

func NewService(users UserStore, tokens *TokenManager) *Service {
	return &Service{users: users, tokens: tokens}
}

func (s *Service) Register(ctx context.Context, email, password string) (string, error) {
	email = normalizeEmail(email)
	if !validEmail(email) || len(password) < minPasswordLen {
		return "", ErrInvalidInput
	}

	hash, err := HashPassword(password)
	if err != nil {
		return "", err
	}

	id, err := s.users.CreateUser(ctx, email, hash)
	if err != nil {
		return "", err
	}
	return s.tokens.Issue(id)
}

func (s *Service) Login(ctx context.Context, email, password string) (string, error) {
	email = normalizeEmail(email)

	user, err := s.users.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return "", ErrInvalidCredentials
		}
		return "", err
	}
	if !CheckPassword(user.PasswordHash, password) {
		return "", ErrInvalidCredentials
	}
	return s.tokens.Issue(user.ID)
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func validEmail(email string) bool {
	at := strings.IndexByte(email, '@')
	return at > 0 && at < len(email)-1 && !strings.ContainsAny(email, " \t")
}
