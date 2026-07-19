package controller

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

func TestSignIframeJWT(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	user := &model.User{Id: 42, Username: "alice", DisplayName: "Alice"}

	signed, err := signIframeJWT(user, "test-secret", now)
	require.NoError(t, err)

	claims := &iframeJWTClaims{}
	parser := jwt.NewParser(jwt.WithTimeFunc(func() time.Time { return now }))
	parsed, err := parser.ParseWithClaims(signed, claims, func(token *jwt.Token) (any, error) {
		require.Equal(t, jwt.SigningMethodHS256, token.Method)
		return []byte("test-secret"), nil
	})
	require.NoError(t, err)
	require.True(t, parsed.Valid)
	require.Equal(t, 42, claims.Id)
	require.Equal(t, "alice", claims.Username)
	require.Equal(t, "Alice", claims.DisplayName)
	require.Equal(t, now.Unix(), claims.IssuedAt.Unix())
	require.Equal(t, now.Add(iframeJWTLifetime).Unix(), claims.ExpiresAt.Unix())
}

func TestSignIframeJWTRequiresSecret(t *testing.T) {
	_, err := signIframeJWT(&model.User{}, "  ", time.Now())
	require.Error(t, err)
}
