.PHONY: loadtest

# CO-505's own testing point: "the test is scripted and repeatable
# (`make loadtest`)". This default target is a fast, safe dry run
# against local Supabase (`supabase start` must already be up) — 8
# simulated hours per developer compressed into well under a minute
# of real wall-clock time via -speed, using the same two seeded users
# (web/e2e/seed.sql) every other local/CI test in this repo already
# relies on. It costs nothing and is always safe to run.
#
# The real scenario the ticket describes — 50 developers x 8 real
# hours against actual Supabase Cloud staging — is a genuinely costly
# action (real Edge Function/Storage/compute spend, and real
# Anthropic API spend if summarize-session ever fires) and is NOT
# what this target runs. See docs/co-505-load-test.md for how to run
# that for real, including the explicit -confirm-real-cost flag the
# tool itself requires before it will target a non-local backend.
loadtest:
	go run ./cmd/carryon-loadtest \
		-api-url http://127.0.0.1:54421 \
		-developers 2 \
		-duration 8h \
		-speed 4800 \
		-pulls-per-hour 120 \
		-network-drop-rate 0.05

