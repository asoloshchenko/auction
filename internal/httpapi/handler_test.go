package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/asoloshchenko/auction/internal/model"
	"github.com/asoloshchenko/auction/internal/service"
	"github.com/google/uuid"
)

type stubAuth struct {
	token string
	err   error
}

func (s stubAuth) Register(context.Context, string, string) (string, error) { return s.token, s.err }
func (s stubAuth) Login(context.Context, string, string) (string, error)    { return s.token, s.err }

type stubAuctions struct {
	store   map[uuid.UUID]model.Auction
	listRes []model.Auction
	err     error
}

func newStubAuctions() *stubAuctions {
	return &stubAuctions{store: make(map[uuid.UUID]model.Auction)}
}

func (s *stubAuctions) CreateAuction(_ context.Context, dto model.CreateAuctionDTO) (uuid.UUID, error) {
	if s.err != nil {
		return uuid.Nil, s.err
	}
	id := uuid.New()
	s.store[id] = model.Auction{
		ID: id, Name: dto.Name, Owner: dto.Owner, StartPrice: dto.StartPrice,
		Step: dto.Step, BuyNowPrice: dto.BuyNowPrice, EndDate: dto.EndDate,
		State: model.AuctionStateActive,
	}
	return id, nil
}

func (s *stubAuctions) GetAuction(_ context.Context, id uuid.UUID) (model.Auction, error) {
	a, ok := s.store[id]
	if !ok {
		return model.Auction{}, service.ErrAuctionNotFound
	}
	return a, nil
}

func (s *stubAuctions) ListAuctions(context.Context, int, int) ([]model.Auction, error) {
	return s.listRes, s.err
}

func (s *stubAuctions) UpdateAuction(context.Context, uuid.UUID, int64, model.UpdateAuctionDTO) error {
	return s.err
}

func (s *stubAuctions) CancelAuction(context.Context, uuid.UUID, int64) error { return s.err }

func (s *stubAuctions) MarkSold(context.Context, uuid.UUID, int64) error { return s.err }

type stubTokens struct {
	userID int64
}

func (s stubTokens) Parse(token string) (int64, error) {
	if token == "good" {
		return s.userID, nil
	}
	return 0, errors.New("bad token")
}

func newServer(t *testing.T, a AuthService, auc AuctionService, userID int64) http.Handler {
	t.Helper()
	h, err := NewServer(a, auc, stubTokens{userID: userID})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return h
}

func req(t *testing.T, h http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

func TestRegister_Created(t *testing.T) {
	h := newServer(t, stubAuth{token: "tok"}, newStubAuctions(), 0)
	rec := req(t, h, http.MethodPost, "/auth/register", "", `{"email":"a@uni.edu","password":"supersecret"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d (%s)", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), `"token":"tok"`) {
		t.Fatalf("token missing: %s", rec.Body)
	}
}

func TestCreateAuction_RequiresAuth(t *testing.T) {
	h := newServer(t, stubAuth{}, newStubAuctions(), 7)
	rec := req(t, h, http.MethodPost, "/auctions", "",
		`{"name":"Bike","startPrice":100,"step":10,"endDate":"2030-01-01T00:00:00Z"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d", rec.Code)
	}
}

func TestCreateAuction_OK(t *testing.T) {
	auc := newStubAuctions()
	h := newServer(t, stubAuth{}, auc, 7)
	rec := req(t, h, http.MethodPost, "/auctions", "good",
		`{"name":"Bike","startPrice":100,"step":10,"endDate":"2030-01-01T00:00:00Z"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d (%s)", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), `"owner":7`) {
		t.Fatalf("owner should come from token, got %s", rec.Body)
	}
}

func TestCreateAuction_ValidationRejected(t *testing.T) {
	h := newServer(t, stubAuth{}, newStubAuctions(), 7)
	rec := req(t, h, http.MethodPost, "/auctions", "good",
		`{"name":"","startPrice":100,"step":0,"endDate":"2030-01-01T00:00:00Z"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for schema violation, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"message"`) || strings.Contains(body, "error_message") {
		t.Fatalf("ogen-internal errors must use the unified Error shape, got %s", body)
	}
}

func TestGetAuction_NotFound(t *testing.T) {
	h := newServer(t, stubAuth{}, newStubAuctions(), 0)
	rec := req(t, h, http.MethodGet, "/auctions/"+uuid.New().String(), "", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestListAuctions_ReturnsItems(t *testing.T) {
	auc := newStubAuctions()
	auc.listRes = []model.Auction{
		{ID: uuid.New(), Name: "A", Owner: 1, StartPrice: 100, Step: 10, EndDate: time.Now(), State: model.AuctionStateActive},
		{ID: uuid.New(), Name: "B", Owner: 2, StartPrice: 50, Step: 5, EndDate: time.Now(), State: model.AuctionStateActive},
	}
	h := newServer(t, stubAuth{}, auc, 0)
	rec := req(t, h, http.MethodGet, "/auctions?limit=10&offset=0", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"limit":10`) || strings.Count(rec.Body.String(), `"name"`) != 2 {
		t.Fatalf("unexpected list body: %s", rec.Body)
	}
}
