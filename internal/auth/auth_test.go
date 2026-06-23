package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/asoloshchenko/auction/internal/model"
)

type fakeUserStore struct {
	users  map[string]model.User
	nextID int64
}

func newFakeUserStore() *fakeUserStore {
	return &fakeUserStore{users: make(map[string]model.User), nextID: 1}
}

func (f *fakeUserStore) CreateUser(_ context.Context, email, passwordHash string) (int64, error) {
	if _, ok := f.users[email]; ok {
		return 0, ErrEmailTaken
	}
	id := f.nextID
	f.nextID++
	f.users[email] = model.User{ID: id, Email: email, PasswordHash: passwordHash}
	return id, nil
}

func (f *fakeUserStore) GetUserByEmail(_ context.Context, email string) (model.User, error) {
	u, ok := f.users[email]
	if !ok {
		return model.User{}, ErrUserNotFound
	}
	return u, nil
}

func newTestService() *Service {
	return NewService(newFakeUserStore(), NewTokenManager("test-secret", time.Hour))
}

func TestHashAndCheckPassword(t *testing.T) {
	hash, err := HashPassword("hunter2pass")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if hash == "hunter2pass" {
		t.Fatal("password must not be stored in plaintext")
	}
	if !CheckPassword(hash, "hunter2pass") {
		t.Fatal("correct password should verify")
	}
	if CheckPassword(hash, "wrongpass") {
		t.Fatal("wrong password must not verify")
	}
}

func TestToken_RoundTrip(t *testing.T) {
	tm := NewTokenManager("secret", time.Hour)
	tok, err := tm.Issue(42)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	id, err := tm.Parse(tok)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if id != 42 {
		t.Fatalf("expected user 42, got %d", id)
	}
}

func TestToken_RejectsWrongSecret(t *testing.T) {
	tok, _ := NewTokenManager("secret-a", time.Hour).Issue(1)
	if _, err := NewTokenManager("secret-b", time.Hour).Parse(tok); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("expected ErrInvalidToken, got %v", err)
	}
}

func TestToken_RejectsExpired(t *testing.T) {
	tm := NewTokenManager("secret", -time.Minute)
	tok, _ := tm.Issue(1)
	if _, err := tm.Parse(tok); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("expected ErrInvalidToken for expired token, got %v", err)
	}
}

func TestRegister_IssuesToken(t *testing.T) {
	svc := newTestService()
	tok, err := svc.Register(context.Background(), "Alice@uni.edu", "supersecret")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if tok == "" {
		t.Fatal("expected a token")
	}
}

func TestRegister_DuplicateEmail(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()
	if _, err := svc.Register(ctx, "a@uni.edu", "supersecret"); err != nil {
		t.Fatalf("first register: %v", err)
	}
	if _, err := svc.Register(ctx, "A@uni.edu", "supersecret"); !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("expected ErrEmailTaken (case-insensitive), got %v", err)
	}
}

func TestRegister_RejectsBadInput(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	if _, err := svc.Register(ctx, "not-an-email", "supersecret"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for bad email, got %v", err)
	}
	if _, err := svc.Register(ctx, "a@uni.edu", "short"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for short password, got %v", err)
	}
}

func TestLogin_Succeeds(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()
	if _, err := svc.Register(ctx, "a@uni.edu", "supersecret"); err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, err := svc.Login(ctx, "a@uni.edu", "supersecret"); err != nil {
		t.Fatalf("login: %v", err)
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()
	if _, err := svc.Register(ctx, "a@uni.edu", "supersecret"); err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, err := svc.Login(ctx, "a@uni.edu", "wrongpassword"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
}

func TestLogin_UnknownEmailHidesExistence(t *testing.T) {
	svc := newTestService()
	if _, err := svc.Login(context.Background(), "ghost@uni.edu", "supersecret"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials (not ErrUserNotFound), got %v", err)
	}
}
