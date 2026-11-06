// Package reader implements CO-109: incrementally tailing a Claude Code
// transcript file (the session's own, or one of its subagent children —
// both are plain JSONL files Carryon never writes to) and detecting when
// one has been truncated or rewritten since it was last read.
package reader

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/devminified/carryon/internal/state"
)

// headHashBytes is how much of a stream file's start ReadNew/Read hash to
// detect a rewrite — CO-109 specifies 4KB.
const headHashBytes = 4096

// ReadNew opens path read-only, seeks to offset, and reads everything up
// to (and including) the last complete line — a partial trailing line
// (Claude Code hasn't finished writing it yet, or we read mid-write) is
// held back, so the returned data is always safe to chunk and upload
// as-is: no line is ever split across two separate reads.
//
// A missing file returns no data and the same offset, not an error —
// Read, the usual caller, can be invoked before Claude Code has created
// the file yet without that being a failure.
func ReadNew(path string, offset int64) (data []byte, newOffset int64, err error) {
	f, err := os.Open(path) // #nosec G304 -- path is caller-controlled (a Claude Code transcript path recorded by internal/hook)
	if errors.Is(err, os.ErrNotExist) {
		return nil, offset, nil
	}
	if err != nil {
		return nil, offset, fmt.Errorf("opening %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return nil, offset, fmt.Errorf("stat %s: %w", path, err)
	}
	if info.Size() <= offset {
		return nil, offset, nil
	}

	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, offset, fmt.Errorf("seeking %s: %w", path, err)
	}
	buf := make([]byte, info.Size()-offset)
	n, err := io.ReadFull(f, buf)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, offset, fmt.Errorf("reading %s: %w", path, err)
	}
	buf = buf[:n]

	lastNewline := bytes.LastIndexByte(buf, '\n')
	if lastNewline < 0 {
		return nil, offset, nil // no complete line yet
	}

	return buf[:lastNewline+1], offset + int64(lastNewline) + 1, nil
}

// Read combines ReadNew with CO-109's rewrite detection: if path's
// current size is smaller than prior.Offset, or the hash of its first 4KB
// no longer matches prior.HeadHash, the file was truncated or rewritten
// since prior was recorded (Claude Code doesn't normally do this, but
// Read defends against it rather than silently uploading a corrupt or
// unrelated range) — in that case it reads from offset 0 instead of
// prior.Offset, so the caller gets this version's content from the
// start. prior being the zero Pointer (never read before) is never
// treated as a rewrite.
//
// Read never mutates prior or persists anything — next is a candidate
// pointer the caller should only pass to state.Save once it has actually
// finished whatever it needed to do with data (CO-109's crash-safety
// testing point: a crash between upload and state write must replay the
// same seq, never skip a range). next.NextSeq is always prior.NextSeq
// unchanged; advancing it on a confirmed upload is the caller's job.
func Read(path string, prior state.Pointer) (data []byte, next state.Pointer, rewritten bool, err error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, prior, false, nil
	}
	if err != nil {
		return nil, prior, false, fmt.Errorf("stat %s: %w", path, err)
	}

	head, err := headHash(path)
	if err != nil {
		return nil, prior, false, err
	}

	readFrom := prior.Offset
	rewritten = prior.HeadHash != "" && (info.Size() < prior.Offset || head != prior.HeadHash)
	if rewritten {
		readFrom = 0
	}

	raw, newOffset, err := ReadNew(path, readFrom)
	if err != nil {
		return nil, prior, false, err
	}

	next = state.Pointer{
		Offset:   newOffset,
		NextSeq:  prior.NextSeq,
		FileSize: info.Size(),
		HeadHash: head,
	}
	return raw, next, rewritten, nil
}

func headHash(path string) (string, error) {
	f, err := os.Open(path) // #nosec G304 -- path is caller-controlled (a Claude Code transcript path recorded by internal/hook)
	if err != nil {
		return "", fmt.Errorf("opening %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	if _, err := io.CopyN(h, f, headHashBytes); err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("hashing %s: %w", path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
