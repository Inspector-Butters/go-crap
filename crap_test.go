package main

import (
	"math"
	"testing"
)

func TestCRAPScore(t *testing.T) {
	tests := []struct {
		name       string
		complexity int
		coverage   float64
		want       float64
	}{
		{name: "simple and covered", complexity: 1, coverage: 1, want: 1},
		{name: "uncovered", complexity: 10, coverage: 0, want: 110},
		{name: "half covered", complexity: 10, coverage: 0.5, want: 22.5},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := crapScore(test.complexity, test.coverage); math.Abs(got-test.want) > 0.0001 {
				t.Fatalf("crapScore(%d, %f) = %f, want %f", test.complexity, test.coverage, got, test.want)
			}
		})
	}
}
