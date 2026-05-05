package util

import (
	"math/rand"

	gamecfg "gserver/gameconfig/gosrc"
)

// weightedRandom 根据权重随机选择一个类型
func WeightedRandom(probs []*gamecfg.GardenProbEntry) int32 {
	total := int32(0)
	for _, p := range probs {
		total += p.Prob
	}
	r := rand.Int31n(total)
	cumulative := int32(0)
	for _, p := range probs {
		cumulative += p.Prob
		if r < cumulative {
			return p.Type
		}
	}
	return probs[len(probs)-1].Type
}