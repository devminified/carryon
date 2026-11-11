'use client';

import { useEffect, useState } from 'react';

// CO-601: the one safe way for a Client Component to show a
// live-ticking "now" without a hydration mismatch. A Client Component
// renders once server-side (building the initial HTML) and once more
// client-side at hydration — if it computed `now` fresh in either of
// those two render passes (e.g. `useState(() => Date.now())`), the
// two passes would see different real instants whenever time elapsed
// between them, and React would flag the resulting text mismatch.
//
// The fix: seed state from a value the server already computed and
// handed down as a prop (so both the SSR pass and the first client
// render use the *identical* instant — no mismatch is possible by
// construction), then only start ticking inside a useEffect, which
// never runs during the render that has to match the server.
export function useLiveNow(initialNow: string, intervalMs = 15_000): Date {
  const [now, setNow] = useState(() => new Date(initialNow));

  useEffect(() => {
    const tick = setInterval(() => setNow(new Date()), intervalMs);
    return () => clearInterval(tick);
  }, [intervalMs]);

  return now;
}
