-- +goose Up
-- +goose StatementBegin
CREATE TABLE users (
    id            BIGSERIAL PRIMARY KEY,
    email         TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE auctions (
    id                   UUID PRIMARY KEY,
    name                 TEXT NOT NULL,
    owner                BIGINT NOT NULL REFERENCES users(id),
    start_price          BIGINT NOT NULL,
    step                 BIGINT NOT NULL,
    buy_now_price        BIGINT,
    end_date             TIMESTAMPTZ NOT NULL,
    state                TEXT NOT NULL,
    winner               BIGINT REFERENCES users(id),
    ending_soon_notified BOOLEAN NOT NULL DEFAULT false,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX idx_auctions_state_end_date ON auctions (state, end_date);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE bets (
    id         UUID PRIMARY KEY,
    auction_id UUID NOT NULL REFERENCES auctions(id) ON DELETE CASCADE,
    owner      BIGINT NOT NULL REFERENCES users(id),
    price      BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX idx_bets_auction_id ON bets (auction_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE bets;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE auctions;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE users;
-- +goose StatementEnd
