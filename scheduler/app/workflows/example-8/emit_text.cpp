#include <cstdint>
#include <cstdio>
#include <emscripten/emscripten.h>

#include "../common/runtime_json.hpp"

namespace {
constexpr int kInputCap = 1024;
constexpr int kOutputCap = 4096;

unsigned char g_input[kInputCap];
char g_output[kOutputCap];
int g_output_len = 0;
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

    // Read the node's own configuration from args.
    const int repeat = static_cast<int>(runtime_json::extract_named_int(in, input_len, "repeat", 8));

    int pos = 0;
    g_output[pos++] = '"';

    // Produce a small text payload that the next node can read from inputs.
    for (int i = 0; i < repeat && pos + 32 < kOutputCap - 2; ++i) {
        pos += std::snprintf(g_output + pos, kOutputCap - pos - 2, "item_%d ", i);
    }

    g_output[pos++] = '"';
    g_output[pos] = '\0';
    g_output_len = pos;
    return 0;
}
}
