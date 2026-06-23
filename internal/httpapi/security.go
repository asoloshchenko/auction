package httpapi

import (
	"context"

	"github.com/asoloshchenko/auction/internal/auth"
	"github.com/asoloshchenko/auction/internal/oas"
)

type securityHandler struct {
	tokens TokenParser
}

var _ oas.SecurityHandler = securityHandler{}

func (h securityHandler) HandleBearerAuth(ctx context.Context, _ oas.OperationName, t oas.BearerAuth) (context.Context, error) {
	userID, err := h.tokens.Parse(t.Token)
	if err != nil {
		return ctx, err
	}
	return auth.ContextWithUserID(ctx, userID), nil
}
