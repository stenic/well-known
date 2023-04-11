package main

import "testing"

func Test_resolveName(t *testing.T) {
	tests := []struct {
		name       string
		annotation string
		want       string
	}{
		{"simple", "well-known.stenic.io/annotation", "annotation"},
		{"slash", "well-known.stenic.io/annotation/dfs", "annotation/dfs"},
		{"empty", "well-known.stenic.io/", ""},
		{"wrong", "bad", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveName(tt.annotation); got != tt.want {
				t.Errorf("resolveName() = %v, want %v", got, tt.want)
			}
		})
	}
}
