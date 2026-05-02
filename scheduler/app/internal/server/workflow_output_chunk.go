package server

import (
	"encoding/json"
	"fmt"
)

func nodeOutputChunkFromValue(value any, hasValue bool, offset int, limit int) (NodeOutputChunkResponse, error) {
	if offset < 0 {
		offset = 0
	}
	if limit < 0 {
		limit = 0
	}
	if !hasValue {
		return NodeOutputChunkResponse{
			Mode:   "missing",
			Offset: offset,
			Limit:  limit,
			Done:   true,
		}, nil
	}

	switch typed := value.(type) {
	case []any:
		if offset > len(typed) {
			offset = len(typed)
		}
		end := boundedEnd(offset, limit, len(typed))
		nextOffset, done := nextChunkOffset(end, len(typed))
		return NodeOutputChunkResponse{
			Mode:       "array",
			Offset:     offset,
			Limit:      limit,
			NextOffset: nextOffset,
			Done:       done,
			TotalItems: len(typed),
			Items:      typed[offset:end],
		}, nil
	case string:
		// Offsets for text are rune-based, not byte-based, so chunking never
		// splits a multi-byte UTF-8 character in the response.
		return nodeOutputTextChunk("string", []rune(typed), offset, limit), nil
	default:
		// Workflow outputs come from JSON payloads/JSONB, so this should usually
		// succeed. Keeping the marshal here gives the caller a clean error if an
		// unexpected non-JSON value is passed in tests or future code paths.
		raw, err := json.Marshal(typed)
		if err != nil {
			return NodeOutputChunkResponse{}, fmt.Errorf("encode output: %w", err)
		}
		// JSON chunks are also rune-based for the same reason as string chunks:
		// client offsets advance by visible characters rather than bytes.
		return nodeOutputTextChunk("json", []rune(string(raw)), offset, limit), nil
	}
}

func nodeOutputTextChunk(mode string, runes []rune, offset int, limit int) NodeOutputChunkResponse {
	if offset < 0 {
		offset = 0
	}
	if limit < 0 {
		limit = 0
	}
	if offset > len(runes) {
		offset = len(runes)
	}
	end := boundedEnd(offset, limit, len(runes))
	nextOffset, done := nextChunkOffset(end, len(runes))
	return NodeOutputChunkResponse{
		Mode:       mode,
		Offset:     offset,
		Limit:      limit,
		NextOffset: nextOffset,
		Done:       done,
		TotalChars: len(runes),
		Data:       string(runes[offset:end]),
	}
}

func boundedEnd(offset int, limit int, total int) int {
	end := offset + limit
	if end > total {
		return total
	}
	return end
}

func nextChunkOffset(end int, total int) (int, bool) {
	done := end >= total
	if done {
		return 0, true
	}
	return end, false
}
