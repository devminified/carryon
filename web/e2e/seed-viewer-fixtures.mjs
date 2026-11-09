// CO-403's own real-verification fixture for the session viewer's
// Playwright suite. Unlike web/e2e/seed.sql (plain rows), this needs
// real zstd-compressed bytes in real Storage — session_chunks' sha256
// must match what's actually stored, and the browser decompresses the
// real object — so this seeds through the real admin APIs (Storage
// upload, table inserts) rather than a SQL-only shortcut. Not part of
// supabase/seed.sql for the same reason web/e2e/seed.sql isn't: only
// web-e2e needs it, and it's sizable, synthetic data.
import { ZstdCodec } from 'zstd-codec';
import { createClient } from '@supabase/supabase-js';
import { createHash } from 'node:crypto';

const SUPABASE_URL = 'http://127.0.0.1:54421';
const SERVICE_ROLE_KEY =
  'eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJzdXBhYmFzZS1kZW1vIiwicm9sZSI6InNlcnZpY2Vfcm9sZSIsImV4cCI6MTk4MzgxMjk5Nn0.EGIM96RAZx35lJzdJsyH-qQwv8Hdp7fsn3W0YpN81IU';

export const VIEWER_PROJECT_ID = '66666666-6666-6666-6666-666666666666';
export const ALI_USER_ID = '11111111-1111-1111-1111-111111111111';
export const PLAIN_SESSION_ID = '77777777-7777-4777-8777-777777777001';
export const REDACTED_SESSION_ID = '77777777-7777-4777-8777-777777777002';
export const SUBAGENT_SESSION_ID = '77777777-7777-4777-8777-777777777003';
export const LARGE_SESSION_ID = '77777777-7777-4777-8777-777777777004';

const PLAIN_LINES = [
  {
    type: 'user',
    uuid: 'u1',
    message: { role: 'user', content: 'Can you fix the payment webhook retry logic?' },
  },
  {
    type: 'assistant',
    uuid: 'a1',
    message: {
      role: 'assistant',
      content: [
        { type: 'text', text: "I'll look at the webhook handler and fix the retry backoff." },
        { type: 'tool_use', id: 'toolu_1', name: 'Read', input: { file_path: '/repo/webhook.ts' } },
      ],
    },
  },
  {
    type: 'user',
    uuid: 'u2',
    message: {
      role: 'user',
      content: [
        {
          type: 'tool_result',
          tool_use_id: 'toolu_1',
          is_error: false,
          content: 'export function retry() { return fetch(url); }\n',
        },
      ],
    },
  },
  {
    type: 'assistant',
    uuid: 'a2',
    message: {
      role: 'assistant',
      content: [
        { type: 'text', text: 'Found it. Rewriting the backoff.' },
        {
          type: 'tool_use',
          id: 'toolu_2',
          name: 'Edit',
          input: {
            file_path: '/repo/webhook.ts',
            old_string: 'export function retry() { return fetch(url); }\n',
            new_string: 'export function retry() {\n  return withBackoff(() => fetch(url));\n}\n',
          },
        },
      ],
    },
  },
  {
    type: 'user',
    uuid: 'u3',
    message: {
      role: 'user',
      content: [{ type: 'tool_result', tool_use_id: 'toolu_2', is_error: false, content: 'ok' }],
    },
  },
  {
    type: 'assistant',
    uuid: 'a3',
    message: {
      role: 'assistant',
      content: [
        { type: 'text', text: "Now let's run the tests." },
        { type: 'tool_use', id: 'toolu_3', name: 'Bash', input: { command: 'npm test' } },
      ],
    },
  },
  {
    type: 'user',
    uuid: 'u4',
    message: {
      role: 'user',
      content: [
        { type: 'tool_result', tool_use_id: 'toolu_3', is_error: true, content: '1 failing\n' },
      ],
    },
  },
  {
    type: 'summary',
    summary: 'Fixed the webhook retry backoff; one test still failing, investigating.',
  },
  { type: 'something_from_the_future', foo: 'bar', baz: [1, 2, 3] },
];

// Already-redacted content — real redaction (internal/redact) runs on
// the laptop before upload; this fixture skips straight to what the
// server would actually have stored, since this script seeds Storage
// directly rather than driving the real CLI upload pipeline.
const REDACTED_LINES = [
  { type: 'user', uuid: 'u1', message: { role: 'user', content: 'check the aws creds' } },
  {
    type: 'assistant',
    uuid: 'a1',
    message: {
      role: 'assistant',
      content: [
        { type: 'text', text: 'Reading the env file' },
        { type: 'tool_use', id: 'toolu_1', name: 'Read', input: { file_path: '/repo/.env.local' } },
      ],
    },
  },
  {
    type: 'user',
    uuid: 'u2',
    message: {
      role: 'user',
      content: [
        {
          type: 'tool_result',
          tool_use_id: 'toolu_1',
          is_error: false,
          content: '[REDACTED:denylisted-file .env.local]',
        },
      ],
    },
  },
];

const SUBAGENT_MAIN_LINES = [
  {
    type: 'user',
    uuid: 'u1',
    message: { role: 'user', content: 'Investigate the flaky test and report back.' },
  },
  {
    type: 'assistant',
    uuid: 'a1',
    message: {
      role: 'assistant',
      content: [
        { type: 'text', text: 'Delegating to a subagent to investigate.' },
        {
          type: 'tool_use',
          id: 'toolu_1',
          name: 'Task',
          input: { description: 'investigate flaky webhook test' },
        },
      ],
    },
  },
  {
    type: 'user',
    uuid: 'u2',
    message: {
      role: 'user',
      content: [
        {
          type: 'tool_result',
          tool_use_id: 'toolu_1',
          is_error: false,
          content: 'Root cause: a race in the retry queue.',
        },
      ],
    },
  },
];

const SUBAGENT_CHILD_LINES = [
  {
    type: 'user',
    uuid: 'su1',
    message: { role: 'user', content: 'investigate flaky webhook test' },
  },
  {
    type: 'assistant',
    uuid: 'sa1',
    message: {
      role: 'assistant',
      content: [{ type: 'text', text: 'Found a race in the retry queue under load.' }],
    },
  },
];

function toJsonl(lines) {
  return lines.map((l) => JSON.stringify(l)).join('\n') + '\n';
}

async function zstdCompress(text) {
  return new Promise((resolve) => {
    ZstdCodec.run((zstd) => {
      const streaming = new zstd.Streaming();
      const bytes = streaming.compress(new TextEncoder().encode(text), 3);
      resolve(Buffer.from(bytes));
    });
  });
}

function sha256Hex(buf) {
  return createHash('sha256').update(buf).digest('hex');
}

function chunkLines(lines, linesPerChunk) {
  const chunks = [];
  for (let i = 0; i < lines.length; i += linesPerChunk) {
    chunks.push(lines.slice(i, i + linesPerChunk));
  }
  return chunks.length > 0 ? chunks : [[]];
}

async function seedSession(
  admin,
  { sessionId, projectId, userId, title, branch, streams, linesPerChunk = 1e9 },
) {
  await admin.from('sessions').upsert({
    id: sessionId,
    project_id: projectId,
    user_id: userId,
    original_cwd: '/repo',
    branch: branch ?? null,
    status: 'ended',
    title: title ?? null,
    total_bytes: 0,
  });

  for (const [streamId, lines] of Object.entries(streams)) {
    let byteStart = 0;
    const lineChunks = chunkLines(lines, linesPerChunk);
    for (let seq = 0; seq < lineChunks.length; seq++) {
      const text = toJsonl(lineChunks[seq]);
      const compressed = await zstdCompress(text);
      const sha256 = sha256Hex(Buffer.from(text, 'utf8'));
      const objectKey = `${projectId}/${userId}/${sessionId}/${streamId}/chunk-${seq}.jsonl.zst`;
      const byteLen = Buffer.byteLength(text, 'utf8');

      const { error: uploadErr } = await admin.storage
        .from('carryon-sessions')
        .upload(objectKey, compressed, { contentType: 'application/zstd', upsert: true });
      if (uploadErr)
        throw new Error(`storage upload failed for ${objectKey}: ${uploadErr.message}`);

      await admin.from('session_chunks').upsert({
        session_id: sessionId,
        stream_id: streamId,
        seq,
        byte_start: byteStart,
        byte_end: byteStart + byteLen,
        object_key: objectKey,
        sha256,
      });
      byteStart += byteLen;
    }
  }
}

// A real multi-chunk session — every other fixture above is one chunk,
// which never exercises streamChunksInOrder's actual seq-ordering logic
// against >1 real chunk. 600 generated exchanges, 40 lines/chunk (~15
// chunks), for a real (if not literally 200MB) progressive-load and
// virtualization check — see this ticket's PR description for why a
// smaller real fixture plus the architecture's Big-O reasoning stands
// in for a literal 200MB run in CI.
function buildLargeSessionLines(exchangeCount) {
  const lines = [];
  for (let i = 0; i < exchangeCount; i++) {
    lines.push({
      type: 'user',
      uuid: `lu${i}`,
      message: { role: 'user', content: `exchange ${i}: please check line ${i} of the config` },
    });
    lines.push({
      type: 'assistant',
      uuid: `la${i}`,
      message: {
        role: 'assistant',
        content: [
          { type: 'text', text: `Looking at exchange ${i}.` },
          {
            type: 'tool_use',
            id: `toolu_${i}`,
            name: 'Read',
            input: { file_path: `/repo/file-${i}.ts` },
          },
        ],
      },
    });
    lines.push({
      type: 'user',
      uuid: `lu2-${i}`,
      message: {
        role: 'user',
        content: [
          {
            type: 'tool_result',
            tool_use_id: `toolu_${i}`,
            is_error: false,
            content: `contents of file-${i}.ts\n`,
          },
        ],
      },
    });
  }
  return lines;
}

async function main() {
  const admin = createClient(SUPABASE_URL, SERVICE_ROLE_KEY);

  await admin.from('projects').upsert({
    id: VIEWER_PROJECT_ID,
    name: 'co403-viewer-fixtures',
    git_remote: 'github.com/devminified/co403-viewer-fixtures',
  });
  await admin.from('project_members').upsert({
    project_id: VIEWER_PROJECT_ID,
    user_id: ALI_USER_ID,
    role: 'admin',
    source: 'manual',
  });

  await seedSession(admin, {
    sessionId: PLAIN_SESSION_ID,
    projectId: VIEWER_PROJECT_ID,
    userId: ALI_USER_ID,
    title: 'fix payment webhook retry logic',
    branch: 'main',
    streams: { main: PLAIN_LINES },
  });

  await seedSession(admin, {
    sessionId: REDACTED_SESSION_ID,
    projectId: VIEWER_PROJECT_ID,
    userId: ALI_USER_ID,
    title: 'check aws creds',
    streams: { main: REDACTED_LINES },
  });

  await seedSession(admin, {
    sessionId: SUBAGENT_SESSION_ID,
    projectId: VIEWER_PROJECT_ID,
    userId: ALI_USER_ID,
    title: 'investigate flaky test',
    streams: { main: SUBAGENT_MAIN_LINES, 'agent-1': SUBAGENT_CHILD_LINES },
  });

  await seedSession(admin, {
    sessionId: LARGE_SESSION_ID,
    projectId: VIEWER_PROJECT_ID,
    userId: ALI_USER_ID,
    title: 'large multi-chunk session',
    streams: { main: buildLargeSessionLines(600) },
    linesPerChunk: 120,
  });

  console.log('viewer fixtures seeded');
}

if (import.meta.url === `file://${process.argv[1]}`) {
  main().catch((err) => {
    console.error(err);
    process.exit(1);
  });
}
