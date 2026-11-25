package gameserver

import (
	"testing"
)

func TestDistributionFactorCacheSeparatesShareBucket(t *testing.T) {
	if distributionFactorCacheKey("rare", 0.5) == distributionFactorCacheKey("rare", 0) {
		t.Fatal("share above and below 5% must use different cache entries")
	}
	g := &fishQueryRegions{areas: []string{"area"}, factors: map[string][]float64{
		distributionFactorCacheKey("rare", 0.5): {0.75},
		distributionFactorCacheKey("rare", 0):   {0},
	}}
	high, err := g.distributionFactors(t.Context(), "rare", 0.5)
	if err != nil {
		t.Fatal(err)
	}
	low, err := g.distributionFactors(t.Context(), "rare", 0)
	if err != nil {
		t.Fatal(err)
	}
	if high[0] != 0.75 || low[0] != 0 {
		t.Fatalf("cache returned wrong factors: high=%v low=%v", high, low)
	}
}
