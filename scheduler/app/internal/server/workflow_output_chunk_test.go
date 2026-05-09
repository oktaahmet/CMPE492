package server

import "testing"

func TestNodeOutputChunkFromValueArray(t *testing.T) {
	t.Parallel()

	chunk, err := nodeOutputChunkFromValue([]any{float64(1), float64(2), float64(3)}, true, 1, 1)
	if err != nil {
		t.Fatalf("nodeOutputChunkFromValue() error = %v", err)
	}
	if chunk.Mode != "array" || chunk.Done || chunk.NextOffset != 2 || chunk.TotalItems != 3 {
		t.Fatalf("unexpected array chunk metadata: %#v", chunk)
	}
	if len(chunk.Items) != 1 || chunk.Items[0] != float64(2) {
		t.Fatalf("unexpected array chunk items: %#v", chunk.Items)
	}
}

func TestNodeOutputChunkFromValueStringUsesRunes(t *testing.T) {
	t.Parallel()

	chunk, err := nodeOutputChunkFromValue("ağb", true, 1, 1)
	if err != nil {
		t.Fatalf("nodeOutputChunkFromValue() error = %v", err)
	}
	if chunk.Mode != "string" || chunk.Data != "ğ" || chunk.TotalChars != 3 || chunk.NextOffset != 2 {
		t.Fatalf("unexpected string chunk: %#v", chunk)
	}
}

func TestNodeOutputChunkFromValueClampsNegativeWindow(t *testing.T) {
	t.Parallel()

	chunk, err := nodeOutputChunkFromValue("abc", true, -10, -1)
	if err != nil {
		t.Fatalf("nodeOutputChunkFromValue() error = %v", err)
	}
	if chunk.Offset != 0 || chunk.Limit != 0 || chunk.Data != "" || chunk.NextOffset != 0 {
		t.Fatalf("unexpected clamped chunk: %#v", chunk)
	}
}

func TestNodeOutputChunkFromValueJSONAndMissing(t *testing.T) {
	t.Parallel()

	chunk, err := nodeOutputChunkFromValue(map[string]any{"ok": true}, true, 0, 20)
	if err != nil {
		t.Fatalf("nodeOutputChunkFromValue(json) error = %v", err)
	}
	if chunk.Mode != "json" || !chunk.Done || chunk.Data != `{"ok":true}` {
		t.Fatalf("unexpected json chunk: %#v", chunk)
	}

	missing, err := nodeOutputChunkFromValue(nil, false, 5, 10)
	if err != nil {
		t.Fatalf("nodeOutputChunkFromValue(missing) error = %v", err)
	}
	if missing.Mode != "missing" || !missing.Done || missing.Offset != 5 || missing.Limit != 10 {
		t.Fatalf("unexpected missing chunk: %#v", missing)
	}
}
