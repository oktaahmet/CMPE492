#include <cstdio>

#include "../common/workflow.hpp"

// WORKFLOW_NODE hides the WASM ABI. The macro provides a `workflow::Input`
// (reads args/inputs/artifacts) and a `workflow::Output` (writes the result
// payload). Default I/O caps are 4 KiB each; use WORKFLOW_NODE_WITH_CAPS if
// you need more.
WORKFLOW_NODE(input, output) {
    // input.int_(key, fallback) reads from args[0] of this node's workflow JSON.
    const int repeat = input.int_("repeat", 8);

    char buf[4096];
    int pos = 0;
    for (int i = 0; i < repeat && pos + 32 < static_cast<int>(sizeof(buf)); ++i) {
        pos += std::snprintf(buf + pos, static_cast<int>(sizeof(buf)) - pos, "item_%d ", i);
    }
    // output.text() JSON-encodes the bytes: it adds the surrounding quotes
    // and escapes anything that would break JSON. Do NOT pre-quote the body.
    output.text(std::string_view(buf, static_cast<size_t>(pos)));
}
