#!/usr/bin/env sh
set -eu

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
WORKFLOWS_DIR="$SCRIPT_DIR/../workflows"
STATIC_DIR="$SCRIPT_DIR/../static"
TARGET_WORKFLOW="${1:-}"

if ! command -v emcc >/dev/null 2>&1; then
  echo "emcc not found in PATH" >&2
  exit 1
fi

if [ ! -d "$WORKFLOWS_DIR" ]; then
  echo "workflows directory missing: $WORKFLOWS_DIR" >&2
  exit 1
fi

mkdir -p "$STATIC_DIR"

build_one() {
  workflow_dir="$1"
  workflow_name="$(basename "$workflow_dir")"
  src_dir="$workflow_dir"
  out_dir="$STATIC_DIR/$workflow_name"

  if [ -d "$workflow_dir/cpp" ]; then
    src_dir="$workflow_dir/cpp"
    out_dir="$STATIC_DIR/uploaded/$workflow_name"
  fi

  found=0
  mkdir -p "$out_dir"
  for src in "$src_dir"/*.cpp; do
    if [ ! -f "$src" ]; then
      continue
    fi
    found=1
    base="$(basename "$src" .cpp)"
    out="$out_dir/$base.wasm"
    echo "Building $out from $src"
    emcc "$src" \
      -O3 \
      -s STANDALONE_WASM=1 \
      -s ALLOW_MEMORY_GROWTH=1 \
      -s INITIAL_MEMORY=67108864 \
      --no-entry \
      -Wl,--export-all \
      -o "$out"
  done

  if [ "$found" -eq 0 ]; then
    echo "Skipping $workflow_name (no .cpp found)" >&2
  fi
}

if [ -n "$TARGET_WORKFLOW" ]; then
  if [ -d "$WORKFLOWS_DIR/$TARGET_WORKFLOW" ]; then
    build_one "$WORKFLOWS_DIR/$TARGET_WORKFLOW"
  elif [ -d "$WORKFLOWS_DIR/uploaded/$TARGET_WORKFLOW" ]; then
    build_one "$WORKFLOWS_DIR/uploaded/$TARGET_WORKFLOW"
  else
    echo "workflow not found: $TARGET_WORKFLOW" >&2
    exit 1
  fi
else
  for dir in "$WORKFLOWS_DIR"/*; do
    [ -d "$dir" ] || continue
    build_one "$dir"
  done
  if [ -d "$WORKFLOWS_DIR/uploaded" ]; then
    for dir in "$WORKFLOWS_DIR/uploaded"/*; do
      [ -d "$dir" ] || continue
      build_one "$dir"
    done
  fi
fi

echo "Done."
