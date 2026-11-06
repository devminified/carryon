-- CO-110: session_chunks needs to distinguish a session's main transcript
-- from its subagent child streams (CO-109's reader.Subagents). Without
-- this, a subagent's chunk seq numbers would collide with the main
-- stream's under the original (session_id, seq) primary key, silently
-- overwriting an unrelated chunk via the existing
-- ON CONFLICT (session_id, seq) DO UPDATE — found while building CO-110,
-- not anticipated by CO-102's original schema.
--
-- stream_id is 'main' for the session's own transcript, or a subagent's
-- agent_id (as parsed from <session>/subagents/agent-<id>.jsonl) for a
-- child stream.

alter table session_chunks drop constraint session_chunks_pkey;

alter table session_chunks add column stream_id text not null default 'main';

alter table session_chunks add primary key (session_id, stream_id, seq);
