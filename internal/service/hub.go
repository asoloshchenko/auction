package service

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/asoloshchenko/auction/internal/model"
	"github.com/coder/websocket"
	"github.com/google/uuid"
)

const (
	sendBuffer   = 32
	writeTimeout = 5 * time.Second
)

type TokenParser interface {
	Parse(token string) (int64, error)
}

type Hub struct {
	tokens TokenParser
	svc    *Service

	mu    sync.RWMutex
	rooms map[uuid.UUID]map[*client]struct{}
	users map[int64]map[*client]struct{}
}

var _ Notifier = (*Hub)(nil)

func NewHub(tokens TokenParser) *Hub {
	return &Hub{
		tokens: tokens,
		rooms:  make(map[uuid.UUID]map[*client]struct{}),
		users:  make(map[int64]map[*client]struct{}),
	}
}

func (h *Hub) Attach(svc *Service) {
	h.svc = svc
}

type client struct {
	conn   *websocket.Conn
	userID int64
	send   chan []byte
	cancel context.CancelFunc
	rooms  map[uuid.UUID]struct{}
}

type inbound struct {
	Type      string    `json:"type"`
	AuctionID uuid.UUID `json:"auctionId"`
	Price     int64     `json:"price"`
}

type event struct {
	Type      string    `json:"type"`
	AuctionID uuid.UUID `json:"auctionId"`
	Price     int64     `json:"price,omitempty"`
	BidderID  int64     `json:"bidderId,omitempty"`
}

type errorEvent struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

type snapshot struct {
	Type         string    `json:"type"`
	AuctionID    uuid.UUID `json:"auctionId"`
	State        string    `json:"state"`
	CurrentPrice int64     `json:"currentPrice"`
	BidCount     int       `json:"bidCount"`
	EndDate      time.Time `json:"endDate"`
	BuyNowPrice  *int64    `json:"buyNowPrice,omitempty"`
	Winner       *int64    `json:"winner,omitempty"`
}

func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	userID, err := h.tokens.Parse(r.URL.Query().Get("token"))
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{"*"}})
	if err != nil {
		return
	}

	ctx, cancel := context.WithCancel(r.Context())
	c := &client{
		conn:   conn,
		userID: userID,
		send:   make(chan []byte, sendBuffer),
		cancel: cancel,
		rooms:  make(map[uuid.UUID]struct{}),
	}

	h.register(c)
	go c.writePump(ctx)
	h.readLoop(ctx, c)

	cancel()
	h.unregister(c)
	conn.CloseNow()
}

func (h *Hub) readLoop(ctx context.Context, c *client) {
	for {
		_, data, err := c.conn.Read(ctx)
		if err != nil {
			return
		}
		h.handleMessage(ctx, c, data)
	}
}

func (h *Hub) handleMessage(ctx context.Context, c *client, data []byte) {
	var in inbound
	if err := json.Unmarshal(data, &in); err != nil {
		c.enqueue(encode(errorEvent{Type: "error", Message: "invalid message"}))
		return
	}

	switch in.Type {
	case "subscribe":
		h.subscribe(c, in.AuctionID)
		h.sendSnapshot(ctx, c, in.AuctionID)
	case "unsubscribe":
		h.unsubscribe(c, in.AuctionID)
	case "bid":
		if h.svc == nil {
			return
		}
		if _, _, err := h.svc.PlaceBet(ctx, model.CreateBetDTO{Owner: c.userID, AuctionID: in.AuctionID, Price: in.Price}); err != nil {
			c.enqueue(encode(errorEvent{Type: "error", Message: err.Error()}))
		}
	default:
		c.enqueue(encode(errorEvent{Type: "error", Message: "unknown message type"}))
	}
}

func (h *Hub) sendSnapshot(ctx context.Context, c *client, id uuid.UUID) {
	a, err := h.svc.GetAuction(ctx, id)
	if err != nil {
		c.enqueue(encode(errorEvent{Type: "error", Message: err.Error()}))
		return
	}

	snap := snapshot{
		Type:         "snapshot",
		AuctionID:    a.ID,
		State:        string(a.State),
		CurrentPrice: a.StartPrice,
		BidCount:     len(a.Bets),
		EndDate:      a.EndDate,
		BuyNowPrice:  a.BuyNowPrice,
		Winner:       a.Winner,
	}
	if h := highestBet(a); h != nil {
		snap.CurrentPrice = h.Price
	}
	c.enqueue(encode(snap))
}

func (h *Hub) BidPlaced(_ context.Context, auctionID uuid.UUID, price, bidderID int64) error {
	h.broadcast(auctionID, event{Type: "bid_placed", AuctionID: auctionID, Price: price, BidderID: bidderID})
	return nil
}

func (h *Hub) BidAccepted(_ context.Context, userID int64, auctionID uuid.UUID, price int64) error {
	h.toUser(userID, event{Type: "bid_accepted", AuctionID: auctionID, Price: price})
	return nil
}

func (h *Hub) Outbid(_ context.Context, userID int64, auctionID uuid.UUID, newPrice int64) error {
	h.toUser(userID, event{Type: "outbid", AuctionID: auctionID, Price: newPrice})
	return nil
}

func (h *Hub) AuctionWon(_ context.Context, userID int64, auctionID uuid.UUID, finalPrice int64) error {
	h.toUser(userID, event{Type: "won", AuctionID: auctionID, Price: finalPrice})
	return nil
}

func (h *Hub) AuctionEndedNoBids(_ context.Context, ownerID int64, auctionID uuid.UUID) error {
	h.toUser(ownerID, event{Type: "ended_no_bids", AuctionID: auctionID})
	return nil
}

func (h *Hub) AuctionEndingSoon(_ context.Context, auctionID uuid.UUID) error {
	h.broadcast(auctionID, event{Type: "ending_soon", AuctionID: auctionID})
	return nil
}

func (h *Hub) broadcast(auctionID uuid.UUID, v any) {
	data := encode(v)
	h.mu.RLock()
	for c := range h.rooms[auctionID] {
		c.enqueue(data)
	}
	h.mu.RUnlock()
}

func (h *Hub) toUser(userID int64, v any) {
	data := encode(v)
	h.mu.RLock()
	for c := range h.users[userID] {
		c.enqueue(data)
	}
	h.mu.RUnlock()
}

func (h *Hub) register(c *client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.users[c.userID] == nil {
		h.users[c.userID] = make(map[*client]struct{})
	}
	h.users[c.userID][c] = struct{}{}
}

func (h *Hub) unregister(c *client) {
	h.mu.Lock()
	delete(h.users[c.userID], c)
	if len(h.users[c.userID]) == 0 {
		delete(h.users, c.userID)
	}
	for auctionID := range c.rooms {
		delete(h.rooms[auctionID], c)
		if len(h.rooms[auctionID]) == 0 {
			delete(h.rooms, auctionID)
		}
	}
	h.mu.Unlock()
	close(c.send)
}

func (h *Hub) subscribe(c *client, auctionID uuid.UUID) {
	h.mu.Lock()
	if h.rooms[auctionID] == nil {
		h.rooms[auctionID] = make(map[*client]struct{})
	}
	h.rooms[auctionID][c] = struct{}{}
	h.mu.Unlock()
	c.rooms[auctionID] = struct{}{}
}

func (h *Hub) unsubscribe(c *client, auctionID uuid.UUID) {
	h.mu.Lock()
	delete(h.rooms[auctionID], c)
	if len(h.rooms[auctionID]) == 0 {
		delete(h.rooms, auctionID)
	}
	h.mu.Unlock()
	delete(c.rooms, auctionID)
}

func (c *client) enqueue(data []byte) {
	select {
	case c.send <- data:
	default:
	}
}

func (c *client) writePump(ctx context.Context) {
	defer c.cancel()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-c.send:
			if !ok {
				return
			}
			wctx, cancel := context.WithTimeout(ctx, writeTimeout)
			err := c.conn.Write(wctx, websocket.MessageText, msg)
			cancel()
			if err != nil {
				return
			}
		}
	}
}

func encode(v any) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return data
}
