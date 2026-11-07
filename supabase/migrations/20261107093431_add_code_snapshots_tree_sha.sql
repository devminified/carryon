-- CO-201: code snapshot capture. register-snapshot needs to store the
-- tree object's SHA alongside the commit's head_sha/wip_ref — it's what
-- lets a later ticket detect "this snapshot's tree is identical to a
-- previous one" server-side too, not just on the laptop that took it.
alter table code_snapshots add column tree_sha text;
