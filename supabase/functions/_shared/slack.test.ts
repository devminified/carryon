import { assert, assertEquals } from "jsr:@std/assert@1.0.19";
import {
  buildBlockedSessionMessage,
  buildDailyDigestMessage,
  buildRescanHitMessage,
  buildSessionUrl,
  postToSlackWithRetry,
} from "./slack.ts";

Deno.test("postToSlackWithRetry: succeeds on the first attempt, no sleep called", async () => {
  let posts = 0;
  let sleeps = 0;
  const ok = await postToSlackWithRetry(
    "https://hooks.slack.com/services/x",
    { text: "hi", blocks: [] },
    {
      post: () => {
        posts++;
        return Promise.resolve();
      },
      sleep: () => {
        sleeps++;
        return Promise.resolve();
      },
    },
  );
  assertEquals(ok, true);
  assertEquals(posts, 1);
  assertEquals(sleeps, 0);
});

Deno.test("postToSlackWithRetry: retries on failure, then succeeds — sleeps between attempts", async () => {
  let posts = 0;
  let sleeps = 0;
  const ok = await postToSlackWithRetry(
    "https://hooks.slack.com/services/x",
    { text: "hi", blocks: [] },
    {
      post: () => {
        posts++;
        if (posts < 3) return Promise.reject(new Error("503"));
        return Promise.resolve();
      },
      sleep: () => {
        sleeps++;
        return Promise.resolve();
      },
    },
  );
  assertEquals(ok, true);
  assertEquals(posts, 3);
  assertEquals(sleeps, 2);
});

Deno.test("postToSlackWithRetry: gives up after every attempt fails, never throws", async () => {
  let posts = 0;
  const ok = await postToSlackWithRetry(
    "https://hooks.slack.com/services/x",
    { text: "hi", blocks: [] },
    {
      post: () => {
        posts++;
        return Promise.reject(new Error("permanently down"));
      },
      sleep: () => Promise.resolve(),
    },
  );
  assertEquals(ok, false);
  assertEquals(posts, 3);
});

Deno.test("buildSessionUrl: builds a link when WEB_APP_URL is configured", () => {
  const prev = Deno.env.get("WEB_APP_URL");
  Deno.env.set("WEB_APP_URL", "https://app.carryon.example/");
  try {
    assertEquals(
      buildSessionUrl("11111111-1111-4111-8111-111111111111"),
      "https://app.carryon.example/sessions/11111111-1111-4111-8111-111111111111",
    );
  } finally {
    if (prev === undefined) Deno.env.delete("WEB_APP_URL");
    else Deno.env.set("WEB_APP_URL", prev);
  }
});

Deno.test("buildSessionUrl: null when WEB_APP_URL isn't configured", () => {
  const prev = Deno.env.get("WEB_APP_URL");
  Deno.env.delete("WEB_APP_URL");
  try {
    assertEquals(buildSessionUrl("s1"), null);
  } finally {
    if (prev !== undefined) Deno.env.set("WEB_APP_URL", prev);
  }
});

Deno.test("buildBlockedSessionMessage: includes the developer, title, link, and reason", () => {
  const msg = buildBlockedSessionMessage({
    sessionTitle: "fix payment webhook retry",
    developerLogin: "ali",
    blockedReason: "failing integration test",
    sessionUrl: "https://app.carryon.example/sessions/s1",
  });
  assert(msg.text.includes("fix payment webhook retry"));
  assert(msg.text.includes("failing integration test"));
  assert(!msg.text.includes("<")); // the plain-text fallback carries no Slack link syntax
  assertEquals(msg.blocks.length, 1);
  const blockText = msg.blocks[0].text?.text ?? "";
  assert(blockText.includes("ali's session"));
  assert(
    blockText.includes(
      "<https://app.carryon.example/sessions/s1|fix payment webhook retry>",
    ),
  );
});

Deno.test("buildBlockedSessionMessage: falls back to 'A session' with no developer login and no link", () => {
  const msg = buildBlockedSessionMessage({
    sessionTitle: "investigate retry queue",
    developerLogin: null,
    blockedReason: "waiting on new API credentials",
    sessionUrl: null,
  });
  assert(msg.text.startsWith("A session"));
  const blockText = msg.blocks[0].text?.text ?? "";
  assert(blockText.includes("*investigate retry queue*"));
  assert(!blockText.includes("<")); // no link syntax when there's no URL
});

Deno.test("buildRescanHitMessage: never includes the matched secret value, only a link", () => {
  const msg = buildRescanHitMessage({
    sessionUrl: "https://app.carryon.example/sessions/s1",
  });
  assert(msg.text.includes("rotate the affected credential"));
  const blockText = msg.blocks[0].text?.text ?? "";
  assert(
    blockText.includes(
      "<https://app.carryon.example/sessions/s1|View session>",
    ),
  );
});

Deno.test("buildRescanHitMessage: degrades gracefully with no session URL configured", () => {
  const msg = buildRescanHitMessage({ sessionUrl: null });
  const blockText = msg.blocks[0].text?.text ?? "";
  assert(!blockText.includes("<"));
});

Deno.test("buildDailyDigestMessage: one line per developer, correct singular/plural", () => {
  const msg = buildDailyDigestMessage({
    projectName: "carryon",
    entries: [
      { developerLogin: "ali", sessionCount: 1 },
      { developerLogin: "sara", sessionCount: 3 },
    ],
  });
  assertEquals(msg.blocks.length, 2);
  assertEquals(msg.blocks[0].text?.type, "plain_text");
  const body = msg.blocks[1].text?.text ?? "";
  assert(body.includes("ali: 1 session\n"));
  assert(body.includes("sara: 3 sessions"));
});

Deno.test("buildDailyDigestMessage: a project with zero entries says so instead of an empty list", () => {
  const msg = buildDailyDigestMessage({ projectName: "carryon", entries: [] });
  const body = msg.blocks[1].text?.text ?? "";
  assertEquals(body, "No session activity in the last 24 hours.");
});
