package adapter

import (
	"context"
	"errors"
	"testing"

	"AgentScope/internal/auth"
	"AgentScope/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type authErrorDB struct {
	err error
}

func (d authErrorDB) Exec(context.Context, string, ...interface{}) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errors.New("unexpected Exec call")
}

func (d authErrorDB) Query(context.Context, string, ...interface{}) (pgx.Rows, error) {
	return nil, errors.New("unexpected Query call")
}

func (d authErrorDB) QueryRow(context.Context, string, ...interface{}) pgx.Row {
	return authErrorRow(d)
}

type authErrorRow struct {
	err error
}

func (r authErrorRow) Scan(...interface{}) error {
	return r.err
}

func TestAuthServiceLoginClassifiesLookupErrors(t *testing.T) {
	dependencyErr := errors.New("database unavailable")
	tests := []struct {
		name              string
		rowErr            error
		wantInvalidCreds  bool
		wantDependencyErr bool
	}{
		{
			name:             "missing user is invalid credentials",
			rowErr:           pgx.ErrNoRows,
			wantInvalidCreds: true,
		},
		{
			name:              "database failure is propagated",
			rowErr:            dependencyErr,
			wantDependencyErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := NewAuthService(store.New(authErrorDB{err: tt.rowErr}))
			_, _, err := service.Login(context.Background(), "person@example.com", "incorrect-password")
			if got := errors.Is(err, auth.ErrInvalidCredentials); got != tt.wantInvalidCreds {
				t.Fatalf("errors.Is(ErrInvalidCredentials) = %t, want %t; err=%v", got, tt.wantInvalidCreds, err)
			}
			if got := errors.Is(err, dependencyErr); got != tt.wantDependencyErr {
				t.Fatalf("errors.Is(dependencyErr) = %t, want %t; err=%v", got, tt.wantDependencyErr, err)
			}
		})
	}
}
