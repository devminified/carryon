-- CO-301: redaction changes byte lengths, so the per-chunk byte_start/
-- byte_end recorded by register-chunk keep referring to the *original*
-- (pre-redaction) file, while the chunk itself holds the redacted
-- bytes. redactions records how many individual replacements
-- (secret-pattern matches, or a denylisted file's whole tool_result)
-- the laptop applied to this one chunk before uploading it — visibility
-- into whether redaction actually did anything, without ever storing
-- what was redacted.

alter table session_chunks add column redactions integer not null default 0;
