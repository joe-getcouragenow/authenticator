package httpapi

import (
	"context"
	"fmt"
	"net/http"

	"github.com/go-kit/kit/log"

	auth "github.com/fmitra/authenticator"
)

type contextKey string

const authorizationHeader = "AUTHORIZATION"
const userIDContextKey contextKey = "userID"

// AuthMiddleware validates an Authorization header if available.
func AuthMiddleware(jsonHandler JSONAPIHandler, tokenSvc auth.TokenService) JSONAPIHandler {
	return func(w http.ResponseWriter, r *http.Request) (interface{}, error) {
		ctx := r.Context()
		jwtToken := r.Header.Get(authorizationHeader)
		if jwtToken == "" {
			return nil, auth.ErrInvalidToken("user is not authenticated")
		}

		token, err := tokenSvc.Validate(ctx, jwtToken)
		if err != nil {
			return nil, err
		}

		ctxWithUserID := context.WithValue(ctx, userIDContextKey, token.UserID)
		r = r.WithContext(ctxWithUserID)

		return jsonHandler(w, r)
	}
}

// ErrorLoggingMiddleware logs any errors that are returned before
// being parsed to an HTTP response.
func ErrorLoggingMiddleware(jsonHandler JSONAPIHandler, source string, log log.Logger) JSONAPIHandler {
	return func(w http.ResponseWriter, r *http.Request) (interface{}, error) {
		userID := GetUserID(r)
		response, err := jsonHandler(w, r)
		if err != nil {
			log.Log(
				"user_id", userID,
				"source", source,
				"error", err.Error(),
				"stack_trace", fmt.Sprintf("%+v", err),
			)
		}
		return response, err
	}
}