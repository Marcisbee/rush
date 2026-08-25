package main

import "testing"

func TestMedian(t *testing.T) {
	if got := median([]float64{9, 1, 4, 2, 8}); got != 4 {
		t.Fatalf("median = %v", got)
	}
}
