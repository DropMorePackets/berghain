package berghain

import "testing"

func TestValidSupportID(t *testing.T) {
	tests := []struct {
		name string
		id   string
		want bool
	}{
		{name: "lowercase", id: "bh@123e4567-e89b-12d3-a456-426614174000", want: true},
		{name: "uppercase", id: "bh@123E4567-E89B-12D3-A456-426614174000", want: true},
		{name: "wrong prefix", id: "foo@123e4567-e89b-12d3-a456-426614174000"},
		{name: "too short", id: "bh@123e4567"},
		{name: "invalid hex", id: "bh@123e4567-e89b-12d3-a456-42661417400z"},
		{name: "invalid separators", id: "bh@123e4567xe89bx12d3xa456x426614174000"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ValidSupportID([]byte(tt.id)); got != tt.want {
				t.Fatalf("ValidSupportID(%q) = %v, want %v", tt.id, got, tt.want)
			}
		})
	}
}
