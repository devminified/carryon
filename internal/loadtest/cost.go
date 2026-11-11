package loadtest

import "time"

// Pricing is Supabase/Anthropic's published, documented-as-approximate
// rate card used to extrapolate a monthly cost from this run's real,
// measured bytes/requests/tokens — same "documented, approximate, not
// a billing API" spirit as supabase/functions/_shared/anthropic.ts's
// own cost_usd column. These are deliberately plain fields on a struct
// (not package constants) so docs/co-505-load-test.md's own report can
// show exactly which rate card a given report used — pricing changes
// over time, and a report that silently used stale numbers would be
// worse than one that states its assumptions.
type Pricing struct {
	// ComputeUSDPerMonth is a Supabase compute add-on's flat monthly
	// price for the plan tier a run assumes — Supabase's own compute
	// pricing is a flat tier, not metered per-request, unlike Storage
	// below.
	ComputeUSDPerMonth float64
	// StorageUSDPerGBMonth is Supabase Storage's per-GB-stored monthly
	// rate.
	StorageUSDPerGBMonth float64
	// StorageUSDPerMillionRequests is Supabase Storage's metered
	// request cost (every chunk PUT/GET is one request).
	StorageUSDPerMillionRequests float64
	// EdgeFunctionUSDPerMillionInvocations is Supabase Edge Functions'
	// metered invocation cost.
	EdgeFunctionUSDPerMillionInvocations float64
	// AnthropicInputUSDPerMillionTokens / OutputUSDPerMillionTokens are
	// Claude Haiku's per-token rate, same tier summarize-session's own
	// cost_usd column assumes.
	AnthropicInputUSDPerMillionTokens  float64
	AnthropicOutputUSDPerMillionTokens float64
}

// DefaultPricing is a reasonable, documented-as-approximate starting
// point — a real run's report should state whichever numbers it
// actually used (see Report.Pricing), and an operator extrapolating a
// real monthly bill should confirm current rates against Supabase's
// and Anthropic's own pricing pages before treating this as final,
// per CO-505's own "required Supabase plan tier is confirmed"
// validation point.
func DefaultPricing() Pricing {
	return Pricing{
		ComputeUSDPerMonth:                   25,
		StorageUSDPerGBMonth:                 0.021,
		StorageUSDPerMillionRequests:         2,
		EdgeFunctionUSDPerMillionInvocations: 2,
		AnthropicInputUSDPerMillionTokens:    0.80,
		AnthropicOutputUSDPerMillionTokens:   4.00,
	}
}

// CostEstimate is what ExtrapolateMonthlyCost produces — one line per
// cost driver, so a report can show its own arithmetic rather than a
// single opaque total.
type CostEstimate struct {
	ComputeUSD         float64
	StorageUSD         float64
	StorageRequestsUSD float64
	EdgeFunctionUSD    float64
	AnthropicUSD       float64
	TotalUSD           float64
}

// ExtrapolateMonthlyCost scales this run's own measured snapshot up
// to developers developers running for one month, at the same per-
// developer rate this run observed. runDuration is the real
// (unscaled) wall-clock time the run's traffic represents — e.g. 8h
// for a full CO-505 run, even if --speed compressed the actual
// wall-clock time spent running it. aiTokens is read separately from
// ai_usage (real token counts), since this run's own traffic doesn't
// call summarize-session itself — see docs/co-505-load-test.md.
func ExtrapolateMonthlyCost(snap Snapshot, developers int, runDuration time.Duration, aiInputTokens, aiOutputTokens int64, pricing Pricing) CostEstimate {
	if runDuration <= 0 || developers <= 0 {
		return CostEstimate{}
	}
	hoursInRun := runDuration.Hours()
	// A developer's own working month: ~21.7 working days/month
	// (52 weeks x 5 days / 12 months) x 8h/day — the same "8 hours
	// each" shape the ticket's own run uses, extrapolated to a month
	// rather than assuming 24/7 usage no laptop agent actually has.
	const workingHoursPerDeveloperPerMonth = 21.7 * 8
	scaleToMonth := workingHoursPerDeveloperPerMonth / hoursInRun

	totalRequests := float64(snap.UploadAttempts + snap.PullAttempts)
	totalBytes := float64(snap.BytesUploaded + snap.BytesDownloaded)

	monthlyRequests := totalRequests * scaleToMonth
	monthlyBytesPerDeveloper := totalBytes * scaleToMonth
	monthlyGBAllDevelopers := monthlyBytesPerDeveloper * float64(developers) / (1024 * 1024 * 1024)
	monthlyRequestsAllDevelopers := monthlyRequests * float64(developers)

	est := CostEstimate{
		ComputeUSD:         pricing.ComputeUSDPerMonth,
		StorageUSD:         monthlyGBAllDevelopers * pricing.StorageUSDPerGBMonth,
		StorageRequestsUSD: (monthlyRequestsAllDevelopers / 1_000_000) * pricing.StorageUSDPerMillionRequests,
		EdgeFunctionUSD:    (monthlyRequestsAllDevelopers / 1_000_000) * pricing.EdgeFunctionUSDPerMillionInvocations,
	}
	if aiInputTokens > 0 || aiOutputTokens > 0 {
		monthlyInputTokens := float64(aiInputTokens) * scaleToMonth * float64(developers)
		monthlyOutputTokens := float64(aiOutputTokens) * scaleToMonth * float64(developers)
		est.AnthropicUSD = (monthlyInputTokens/1_000_000)*pricing.AnthropicInputUSDPerMillionTokens +
			(monthlyOutputTokens/1_000_000)*pricing.AnthropicOutputUSDPerMillionTokens
	}
	est.TotalUSD = est.ComputeUSD + est.StorageUSD + est.StorageRequestsUSD + est.EdgeFunctionUSD + est.AnthropicUSD
	return est
}
