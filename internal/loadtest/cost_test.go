package loadtest

import (
	"testing"
	"time"
)

func TestExtrapolateMonthlyCostZeroInputsIsZero(t *testing.T) {
	got := ExtrapolateMonthlyCost(Snapshot{}, 0, time.Hour, 0, 0, DefaultPricing())
	if got.TotalUSD != 0 {
		t.Fatalf("0 developers should extrapolate to $0, got %+v", got)
	}
	got = ExtrapolateMonthlyCost(Snapshot{}, 50, 0, 0, 0, DefaultPricing())
	if got.TotalUSD != 0 {
		t.Fatalf("0 runDuration should extrapolate to $0, got %+v", got)
	}
}

func TestExtrapolateMonthlyCostScalesWithDevelopers(t *testing.T) {
	snap := Snapshot{
		UploadAttempts:  100,
		PullAttempts:    10,
		BytesUploaded:   10 * 1024 * 1024,
		BytesDownloaded: 10 * 1024 * 1024,
	}
	one := ExtrapolateMonthlyCost(snap, 1, 8*time.Hour, 0, 0, DefaultPricing())
	fifty := ExtrapolateMonthlyCost(snap, 50, 8*time.Hour, 0, 0, DefaultPricing())

	// Storage/request/edge-function costs should scale ~50x (compute
	// is a flat tier and deliberately doesn't), so the total for 50
	// developers must exceed 1 developer's by a comparable factor,
	// not just be marginally larger.
	if fifty.StorageUSD <= one.StorageUSD*40 {
		t.Fatalf("StorageUSD did not scale with developer count: one=%v fifty=%v", one.StorageUSD, fifty.StorageUSD)
	}
	if fifty.ComputeUSD != one.ComputeUSD {
		t.Fatalf("ComputeUSD should be a flat monthly tier, independent of developer count: one=%v fifty=%v", one.ComputeUSD, fifty.ComputeUSD)
	}
	if fifty.TotalUSD <= one.TotalUSD {
		t.Fatalf("fifty.TotalUSD (%v) should exceed one.TotalUSD (%v)", fifty.TotalUSD, one.TotalUSD)
	}
}

func TestExtrapolateMonthlyCostIncludesAnthropicOnlyWhenTokensPresent(t *testing.T) {
	withoutTokens := ExtrapolateMonthlyCost(Snapshot{}, 1, 8*time.Hour, 0, 0, DefaultPricing())
	if withoutTokens.AnthropicUSD != 0 {
		t.Fatalf("AnthropicUSD should be 0 with no token counts, got %v", withoutTokens.AnthropicUSD)
	}
	withTokens := ExtrapolateMonthlyCost(Snapshot{}, 1, 8*time.Hour, 1000, 500, DefaultPricing())
	if withTokens.AnthropicUSD <= 0 {
		t.Fatalf("AnthropicUSD should be > 0 once token counts are supplied, got %v", withTokens.AnthropicUSD)
	}
}
