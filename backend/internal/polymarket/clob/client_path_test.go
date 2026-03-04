package clob

import "testing"

func TestIsBookEndpointPath(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{name: "plain book path", in: "/book", want: true},
		{name: "double slash book path", in: "//book", want: true},
		{name: "book path with prefix", in: "/v2/book", want: true},
		{name: "book path with nested double slash", in: "/v2//book", want: true},
		{name: "book path with trailing slash", in: "/book/", want: true},
		{name: "different endpoint", in: "/order", want: false},
		{name: "similar suffix but not endpoint", in: "/orderbook", want: false},
		{name: "empty path", in: "", want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isBookEndpointPath(tc.in)
			if got != tc.want {
				t.Fatalf("isBookEndpointPath(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}
