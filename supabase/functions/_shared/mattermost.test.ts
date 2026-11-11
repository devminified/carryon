import { assert, assertEquals } from "jsr:@std/assert@1.0.19";
import {
  buildBlockedSessionMessage,
  buildDailyDigestMessage,
  buildRescanHitMessage,
  postToMattermostWithRetry,
} from "./mattermost.ts";

Deno.test("postToMattermostWithRetry: succeeds on the first attempt, no sleep called", async () => {
  let posts = 0;
  let sleeps = 0;
  const ok = await postToMattermostWithRetry(
    "http://mattermost.example/hooks/x",
    { text: "hi" },
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

Deno.test("postToMattermostWithRetry: retries on failure, then succeeds — sleeps between attempts", async () => {
  let posts = 0;
  let sleeps = 0;
  const ok = await postToMattermostWithRetry(
    "http://mattermost.example/hooks/x",
    { text: "hi" },
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

Deno.test("postToMattermostWithRetry: gives up after every attempt fails, never throws", async () => {
  let posts = 0;
  const ok = await postToMattermostWithRetry(
    "http://mattermost.example/hooks/x",
    { text: "hi" },
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

// The real payload shape and markdown dialect here were verified
// against a real local Mattermost instance (mattermost/mattermost-
// preview) before writing any of this: Mattermost ignores Slack's
// Block Kit `blocks` array entirely, but renders a Slack-attachment-
// compatible `attachments[].text` using its own Markdown
// (`**bold**`, `[text](url)` links — not Slack's `*bold*`/
// `<url|text>`). These tests assert on that real, verified shape, not
// an assumed one.

Deno.test("buildBlockedSessionMessage: includes the developer, title, link, and reason, using Mattermost's own markdown", () => {
  const msg = buildBlockedSessionMessage({
    sessionTitle: "fix payment webhook retry",
    developerLogin: "ali",
    blockedReason: "failing integration test",
    sessionUrl: "https://app.carryon.example/sessions/s1",
  });
  assert(msg.text.includes("fix payment webhook retry"));
  assert(msg.text.includes("failing integration test"));
  assert(!msg.text.includes("[")); // the plain-text fallback carries no link syntax
  assertEquals(msg.attachments?.length, 1);
  const body = msg.attachments?.[0].text ?? "";
  assert(body.includes("ali's session"));
  assert(
    body.includes(
      "[fix payment webhook retry](https://app.carryon.example/sessions/s1)",
    ),
  );
  // Slack's own link/bold syntax must never leak into a Mattermost payload.
  assert(!body.includes("<https://"));
});

Deno.test("buildBlockedSessionMessage: falls back to 'A session' with no developer login and no link", () => {
  const msg = buildBlockedSessionMessage({
    sessionTitle: "investigate retry queue",
    developerLogin: null,
    blockedReason: "waiting on new API credentials",
    sessionUrl: null,
  });
  assert(msg.text.startsWith("A session"));
  const body = msg.attachments?.[0].text ?? "";
  assert(body.includes("**investigate retry queue**"));
  assert(!body.includes("[")); // no link syntax when there's no URL
});

Deno.test("buildRescanHitMessage: never includes the matched secret value, only a link", () => {
  const msg = buildRescanHitMessage({
    sessionUrl: "https://app.carryon.example/sessions/s1",
  });
  assert(msg.text.includes("rotate the affected credential"));
  const body = msg.attachments?.[0].text ?? "";
  assert(
    body.includes("[View session](https://app.carryon.example/sessions/s1)"),
  );
});

Deno.test("buildRescanHitMessage: degrades gracefully with no session URL configured", () => {
  const msg = buildRescanHitMessage({ sessionUrl: null });
  const body = msg.attachments?.[0].text ?? "";
  assert(!body.includes("["));
});

Deno.test("buildDailyDigestMessage: one line per developer, correct singular/plural", () => {
  const msg = buildDailyDigestMessage({
    projectName: "carryon",
    entries: [
      { developerLogin: "ali", sessionCount: 1 },
      { developerLogin: "sara", sessionCount: 3 },
    ],
  });
  const body = msg.attachments?.[0].text ?? "";
  assert(body.includes("ali: 1 session\n"));
  assert(body.includes("sara: 3 sessions"));
  assert(body.includes("**Daily digest — carryon**"));
});

Deno.test("buildDailyDigestMessage: a project with zero entries says so instead of an empty list", () => {
  const msg = buildDailyDigestMessage({ projectName: "carryon", entries: [] });
  const body = msg.attachments?.[0].text ?? "";
  assert(body.endsWith("No session activity in the last 24 hours."));
});
