#include <cstdint>
#include <cstdio>
#include <cstring>
#include <emscripten/emscripten.h>

#include "../common/runtime_json.hpp"

namespace {
constexpr int kInputCap = 4096;
constexpr int kOutputCap = 1024;

unsigned char g_input[kInputCap];
char g_output[kOutputCap];
int g_output_len = 0;

int count_token(const char* data, int len, const char* token) {
    const int token_len = static_cast<int>(std::strlen(token));
    if (token_len <= 0 || len < token_len) {
        return 0;
    }

    int count = 0;
    for (int i = 0; i + token_len <= len; ++i) {
        bool match = true;
        for (int j = 0; j < token_len; ++j) {
            if (data[i + j] != token[j]) {
                match = false;
                break;
            }
        }
        if (match) {
            ++count;
            i += token_len - 1;
        }
    }
    return count;
}
}  // namespace

extern "C" {
EMSCRIPTEN_KEEPALIVE int get_input_ptr() { return static_cast<int>(reinterpret_cast<uintptr_t>(g_input)); }
EMSCRIPTEN_KEEPALIVE int get_input_capacity() { return kInputCap; }
EMSCRIPTEN_KEEPALIVE int get_output_ptr() { return static_cast<int>(reinterpret_cast<uintptr_t>(g_output)); }
EMSCRIPTEN_KEEPALIVE int get_output_len() { return g_output_len; }

EMSCRIPTEN_KEEPALIVE int run_json(int input_len) {
    if (input_len < 0 || input_len > kInputCap) {
        return 1;
    }

    const char* in = reinterpret_cast<const char*>(g_input);

    // This node still has its own args even though it also consumes an input.
    const int passes = static_cast<int>(runtime_json::extract_named_int(in, input_len, "passes", 2));

    // Read the upstream node output from inputs["emit-text"].output.
    const char* upstream = nullptr;
    const int text_len = runtime_json::extract_input_output_string(in, input_len, "emit-text", &upstream);
    const int item_count = upstream ? count_token(upstream, text_len, "item_") : 0;

    // Return a compact summary string as this node's output.
    g_output_len = std::snprintf(
        g_output,
        kOutputCap,
        "\"passes_%d bytes_%d items_%d\"",
        passes,
        text_len,
        item_count);
    if (g_output_len < 0 || g_output_len >= kOutputCap) {
        g_output_len = 0;
        return 2;
    }
    return 0;
}
}
