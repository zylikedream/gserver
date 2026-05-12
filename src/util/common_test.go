package util

import (
	"testing"

	gamecfg "gserver/gameconfig/gosrc"
)

func TestWeightedRandom_Single(t *testing.T) {
	probs := []*gamecfg.GardenProbEntry{{Type: 1, Prob: 100}}
	for i := 0; i < 10; i++ {
		if got := WeightedRandom(probs); got != 1 {
			t.Fatalf("expected 1, got %d", got)
		}
	}
}

func TestWeightedRandom_Distribution(t *testing.T) {
	probs := []*gamecfg.GardenProbEntry{
		{Type: 1, Prob: 70},
		{Type: 2, Prob: 30},
	}
	counts := map[int32]int{}
	const n = 10000
	for i := 0; i < n; i++ {
		counts[WeightedRandom(probs)]++
	}
	// 70:30 ratio, allow ±10%
	ratio := float64(counts[1]) / float64(counts[2])
	if ratio < 1.5 || ratio > 3.5 {
		t.Fatalf("expected ~2.33 ratio, got %.2f (counts: %v)", ratio, counts)
	}
}

func TestWeightedRandom_ThreeWay(t *testing.T) {
	probs := []*gamecfg.GardenProbEntry{
		{Type: 10, Prob: 50},
		{Type: 20, Prob: 30},
		{Type: 30, Prob: 20},
	}
	counts := map[int32]int{}
	const n = 10000
	for i := 0; i < n; i++ {
		counts[WeightedRandom(probs)]++
	}
	for _, typ := range []int32{10, 20, 30} {
		if counts[typ] == 0 {
			t.Fatalf("type %d never selected", typ)
		}
	}
}
