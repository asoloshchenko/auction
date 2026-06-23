package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/asoloshchenko/auction/internal/oas"
	"github.com/coder/websocket"
	"github.com/google/uuid"
)

type tokenSource struct{ token string }

func (t *tokenSource) BearerAuth(_ context.Context, _ oas.OperationName) (oas.BearerAuth, error) {
	return oas.BearerAuth{Token: t.token}, nil
}

func main() {
	base := flag.String("base", "http://localhost:8080", "server base URL")
	flag.Parse()
	if err := run(*base); err != nil {
		log.Fatal(err)
	}
}

func run(base string) error {
	ctx := context.Background()

	sellerTok, bidderTok := &tokenSource{}, &tokenSource{}
	seller, err := oas.NewClient(base, sellerTok)
	if err != nil {
		return err
	}
	bidder, err := oas.NewClient(base, bidderTok)
	if err != nil {
		return err
	}

	step("register seller and bidder")
	if sellerTok.token, err = authToken(ctx, seller, "seller@uni.edu", "supersecret"); err != nil {
		return err
	}
	if bidderTok.token, err = authToken(ctx, bidder, "bidder@uni.edu", "supersecret"); err != nil {
		return err
	}
	fmt.Println("  seller and bidder authenticated")

	step("seller creates auction (buyNow=500)")
	auction, err := seller.CreateAuction(ctx, &oas.CreateAuctionRequest{
		Name:        "Bike",
		StartPrice:  100,
		Step:        10,
		BuyNowPrice: oas.NewOptInt64(500),
		EndDate:     time.Now().Add(5 * time.Minute),
	})
	if err != nil {
		return err
	}
	fmt.Printf("  id=%s state=%s currentPrice=%d\n", auction.ID, auction.State, auction.CurrentPrice)

	step("validation is server-side (step=0 -> error)")
	_, badErr := seller.CreateAuction(ctx, &oas.CreateAuctionRequest{
		Name: "Bad", StartPrice: 100, Step: 0, EndDate: time.Now().Add(time.Minute),
	})
	fmt.Printf("  rejected as expected: %v\n", badErr)

	step("bidder hits Buy Now over WebSocket")
	if err := buyNowBid(ctx, base, bidderTok.token, auction.ID); err != nil {
		return err
	}

	step("auction is now ENDED with a winner")
	ended, err := seller.GetAuction(ctx, oas.GetAuctionParams{ID: auction.ID})
	if err != nil {
		return err
	}
	fmt.Printf("  state=%s winner=%v currentPrice=%d\n", ended.State, ended.Winner.Value, ended.CurrentPrice)

	step("owner marks it SOLD")
	sold, err := seller.MarkAuctionSold(ctx, oas.MarkAuctionSoldParams{ID: auction.ID})
	if err != nil {
		return err
	}
	fmt.Printf("  state=%s\n", sold.State)

	step("done")
	return nil
}

func authToken(ctx context.Context, c *oas.Client, email, password string) (string, error) {
	creds := &oas.Credentials{Email: email, Password: password}
	if reg, err := c.RegisterUser(ctx, creds); err == nil {
		return reg.Token, nil
	}
	resp, err := c.LoginUser(ctx, creds)
	if err != nil {
		return "", fmt.Errorf("login %s: %w", email, err)
	}
	return resp.Token, nil
}

func buyNowBid(ctx context.Context, base, token string, id uuid.UUID) error {
	wsURL := strings.Replace(base, "http", "ws", 1) + "/ws?token=" + token
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		return err
	}
	defer conn.CloseNow()

	for _, msg := range []map[string]any{
		{"type": "subscribe", "auctionId": id.String()},
		{"type": "bid", "auctionId": id.String(), "price": 500},
	} {
		data, _ := json.Marshal(msg)
		if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
			return err
		}
	}

	readCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	for {
		_, data, err := conn.Read(readCtx)
		if err != nil {
			return nil
		}
		fmt.Printf("  ws <- %s\n", data)
	}
}

func step(title string) {
	fmt.Printf("\n=== %s ===\n", title)
}
