package util

import (
	"testing"

	gamecfg "gserver/gameconfig/gosrc"
)

// TestWeightedRandom_Single:契约——单一权重项(Prob=100)时 10 次抽样必须恒返回该 Type,
// 且不依赖 rand.Int31n 的随机数落在哪一段。
// 为什么需要:该 Type 直接决定居民订单要几种花材(needKindCount),返回 0 会让
// randomDemands 抽 0 个需求,玩家拿到一个没有任何需求项的空订单,永远无法交付。
func TestWeightedRandom_Single(t *testing.T) {
	probs := []*gamecfg.GardenProbEntry{{Type: 1, Prob: 100}}
	for range 10 {
		if got := WeightedRandom(probs); got != 1 {
			t.Fatalf("expected 1, got %d", got)
		}
	}
}

// TestWeightedRandom_Distribution:契约——70/30 两项、10000 次抽样,counts[1]/counts[2]
// 必须落在 [1.5, 3.5](名义 70:30 ≈ 2.33,留了足够的采样抖动余量)。
// 为什么需要:权重配错(如累计和比较写成 <= 或权重累加漏项)不会报错,只会让玩家
// 接到与策划配置不符的花材组合比例,掉落经济整体偏移。
func TestWeightedRandom_Distribution(t *testing.T) {
	probs := []*gamecfg.GardenProbEntry{
		{Type: 1, Prob: 70},
		{Type: 2, Prob: 30},
	}
	counts := map[int32]int{}
	const n = 10000
	for range n {
		counts[WeightedRandom(probs)]++
	}
	// 70:30 ratio, allow ±10%
	ratio := float64(counts[1]) / float64(counts[2])
	if ratio < 1.5 || ratio > 3.5 {
		t.Fatalf("expected ~2.33 ratio, got %.2f (counts: %v)", ratio, counts)
	}
}

// TestWeightedRandom_ThreeWay:契约——50/30/20 三项、10000 次抽样,三个 Type 都至少命中一次,
// 保护的是"每个分桶都可达";比例形状由 TestWeightedRandom_Distribution 断言,这里不断言。
// 为什么需要:桶数从 2 增到 3 后,越界或累计偏移会让中间某个桶永远抽不到(该花材绝版),
// 或全部落到末尾兜底分支 probs[len-1].Type;两者都不报错,只在产出里体现。
func TestWeightedRandom_ThreeWay(t *testing.T) {
	probs := []*gamecfg.GardenProbEntry{
		{Type: 10, Prob: 50},
		{Type: 20, Prob: 30},
		{Type: 30, Prob: 20},
	}
	counts := map[int32]int{}
	const n = 10000
	for range n {
		counts[WeightedRandom(probs)]++
	}
	for _, typ := range []int32{10, 20, 30} {
		if counts[typ] == 0 {
			t.Fatalf("type %d never selected", typ)
		}
	}
}
