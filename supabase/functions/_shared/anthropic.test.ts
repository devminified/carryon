import { assertEquals, assertRejects } from "jsr:@std/assert@1.0.19";
import {
  AnthropicAPIError,
  callAnthropic,
  estimateCostUSD,
} from "./anthropic.ts";

const originalFetch = globalThis.fetch;

// A narrow, local signature rather than `typeof fetch` — the global
// `fetch`/`RequestInit` types get augmented by other files' own
// imports (supabase-js's HTTP client typings) once `deno test` type-
// checks this whole directory together, which this file has no
// control over; body/headers are literally all callAnthropic ever
// passes, so that's all this stub needs to know about.
type FetchInit = { body?: string; headers?: HeadersInit };
function stubFetch(impl: (url: string, init: FetchInit) => Promise<Response>) {
  // deno-lint-ignore no-explicit-any
  globalThis.fetch = impl as any;
}
function restoreFetch() {
  globalThis.fetch = originalFetch;
}

Deno.test("callAnthropic sends the model, system, and user message, and parses the text + usage back", async () => {
  const captured: {
    body: Record<string, unknown> | null;
    headers: Headers | null;
  } = {
    body: null,
    headers: null,
  };
  stubFetch((_url, init) => {
    captured.body = JSON.parse(init.body as string);
    captured.headers = new Headers(init.headers);
    return Promise.resolve(
      new Response(
        JSON.stringify({
          content: [{ type: "text", text: '{"title":"fix the bug"}' }],
          usage: { input_tokens: 120, output_tokens: 40 },
        }),
        { status: 200 },
      ),
    );
  });
  try {
    const result = await callAnthropic(
      "real-key",
      "system prompt",
      "user prompt",
    );
    assertEquals(result.text, '{"title":"fix the bug"}');
    assertEquals(result.usage, { inputTokens: 120, outputTokens: 40 });
    assertEquals(captured.headers?.get("x-api-key"), "real-key");
    assertEquals(captured.body?.system, "system prompt");
    const messages = captured.body?.messages as
      | Array<{ content: string }>
      | undefined;
    assertEquals(messages?.[0]?.content, "user prompt");
  } finally {
    restoreFetch();
  }
});

Deno.test("callAnthropic throws AnthropicAPIError on a non-2xx response, without the key ever appearing in the error", async () => {
  stubFetch(() =>
    Promise.resolve(new Response("rate limited", { status: 429 }))
  );
  try {
    await assertRejects(
      () => callAnthropic("super-secret-key", "s", "u"),
      AnthropicAPIError,
    );
  } finally {
    restoreFetch();
  }
});

Deno.test("callAnthropic tolerates a missing usage block, defaulting to zero tokens", async () => {
  stubFetch(() =>
    Promise.resolve(
      new Response(
        JSON.stringify({ content: [{ type: "text", text: "ok" }] }),
        {
          status: 200,
        },
      ),
    )
  );
  try {
    const result = await callAnthropic("k", "s", "u");
    assertEquals(result.usage, { inputTokens: 0, outputTokens: 0 });
  } finally {
    restoreFetch();
  }
});

Deno.test("callAnthropic joins multiple text content blocks", async () => {
  stubFetch(() =>
    Promise.resolve(
      new Response(
        JSON.stringify({
          content: [
            { type: "text", text: "part one " },
            { type: "text", text: "part two" },
          ],
          usage: { input_tokens: 1, output_tokens: 1 },
        }),
        { status: 200 },
      ),
    )
  );
  try {
    const result = await callAnthropic("k", "s", "u");
    assertEquals(result.text, "part one part two");
  } finally {
    restoreFetch();
  }
});

Deno.test("estimateCostUSD is a plain, documented approximation (input cheaper than output)", () => {
  const cost = estimateCostUSD({ inputTokens: 1_000_000, outputTokens: 0 });
  const cost2 = estimateCostUSD({ inputTokens: 0, outputTokens: 1_000_000 });
  if (!(cost > 0 && cost2 > cost)) {
    throw new Error(`expected 0 < cost (${cost}) < cost2 (${cost2})`);
  }
});

Deno.test("estimateCostUSD of zero usage is zero", () => {
  assertEquals(estimateCostUSD({ inputTokens: 0, outputTokens: 0 }), 0);
});
