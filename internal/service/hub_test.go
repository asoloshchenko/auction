package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

func testClient(userID int64, buf int) *client {
	return &client{
		userID: userID,
		send:   make(chan []byte, buf),
		cancel: func() {},
		rooms:  make(map[uuid.UUID]struct{}),
	}
}

func recvType(t *testing.T, c *client) string {
	t.Helper()
	select {
	case data := <-c.send:
		var e struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(data, &e); err != nil {
			t.Fatalf("bad event json: %v", err)
		}
		return e.Type
	case <-time.After(time.Second):
		t.Fatal("expected a message, got none")
		return ""
	}
}

func assertSilent(t *testing.T, c *client) {
	t.Helper()
	select {
	case data := <-c.send:
		t.Fatalf("expected no message, got %s", data)
	default:
	}
}

func TestHub_BidPlacedBroadcastsToSubscribers(t *testing.T) {
	h := NewHub(nil)
	a, b := uuid.New(), uuid.New()
	c1, c2, c3 := testClient(1, 8), testClient(2, 8), testClient(3, 8)
	h.register(c1)
	h.register(c2)
	h.register(c3)
	h.subscribe(c1, a)
	h.subscribe(c2, a)
	h.subscribe(c3, b)

	_ = h.BidPlaced(context.Background(), a, 150, 9)

	if got := recvType(t, c1); got != "bid_placed" {
		t.Fatalf("c1: expected bid_placed, got %s", got)
	}
	if got := recvType(t, c2); got != "bid_placed" {
		t.Fatalf("c2: expected bid_placed, got %s", got)
	}
	assertSilent(t, c3)
}

func TestHub_OutbidTargetsUserOnly(t *testing.T) {
	h := NewHub(nil)
	loser, other := testClient(7, 8), testClient(8, 8)
	h.register(loser)
	h.register(other)

	_ = h.Outbid(context.Background(), 7, uuid.New(), 200)

	if got := recvType(t, loser); got != "outbid" {
		t.Fatalf("expected outbid, got %s", got)
	}
	assertSilent(t, other)
}

func TestHub_EnqueueDropsWhenFull(t *testing.T) {
	c := testClient(1, 1)
	c.enqueue([]byte("a"))
	c.enqueue([]byte("b"))

	if len(c.send) != 1 {
		t.Fatalf("expected one buffered message, got %d", len(c.send))
	}
}

func TestHub_UnregisterStopsDelivery(t *testing.T) {
	h := NewHub(nil)
	a := uuid.New()
	c := testClient(1, 8)
	h.register(c)
	h.subscribe(c, a)

	h.unregister(c)

	_ = h.BidPlaced(context.Background(), a, 1, 1)

	if _, ok := <-c.send; ok {
		t.Fatal("send channel should be closed and empty after unregister")
	}
}
