package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestResolveOrder verifies the order reconciliation: a fresh database keeps the
// configured order, a matching order does not warn, and any mismatch adopts the
// database's order while flagging a warning.
func TestResolveOrder(t *testing.T) {
	tests := []struct {
		name         string
		configured   int
		dbOrder      int
		hasData      bool
		wantOrder    int
		wantMismatch bool
	}{
		{"fresh database keeps the configured order", 3, 0, false, 3, false},
		{"matching order does not warn", 3, 3, true, 3, false},
		{"mismatch adopts the database order", 2, 3, true, 3, true},
		{"mismatch in the other direction", 4, 2, true, 2, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			order, mismatch := resolveOrder(tt.configured, tt.dbOrder, tt.hasData)
			require.Equal(t, tt.wantOrder, order)
			require.Equal(t, tt.wantMismatch, mismatch)
		})
	}
}
