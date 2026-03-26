#include <cstdint>
#include <cstdio>
#include <emscripten/emscripten.h>

#include "../common/runtime_json.hpp"

namespace {
constexpr int kInputCap = 8 * 1024 * 1024;
constexpr int kOutputCap = 3 * 1024 * 1024;
constexpr int kMinTargetBytes = 200000;
constexpr int kMaxTargetBytes = 2200000;

unsigned char g_input[kInputCap];
char g_output[kOutputCap];
int g_output_len = 0;

uint32_t next_lcg(uint32_t state) {
    return state * 1664525u + 1013904223u;
}

const char* domain_name(int id) {
    if (id == 0) return "technews";
    if (id == 1) return "financedaily";
    return "sciencewire";
}

const char* topic_name(uint32_t t) {
    switch (t % 5u) {
        case 0: return "ai";
        case 1: return "macro";
        case 2: return "security";
        case 3: return "health";
        default: return "climate";
    }
}

int write_snapshot_json_string(int target_bytes, uint32_t seed, int domain_count, int bias_ai) {
    if (target_bytes < kMinTargetBytes) {
        target_bytes = kMinTargetBytes;
    }
    if (target_bytes > kMaxTargetBytes) {
        target_bytes = kMaxTargetBytes;
    }
    if (domain_count < 1) {
        domain_count = 1;
    }
    if (domain_count > 3) {
        domain_count = 3;
    }
    if (bias_ai < 0) {
        bias_ai = 0;
    }
    if (bias_ai > 100) {
        bias_ai = 100;
    }

    int pos = 0;
    g_output[pos++] = '"';
    uint32_t state = seed == 0 ? 20260301u : seed;
    int doc = 0;

    while (pos + 256 < kOutputCap - 2 && pos < target_bytes) {
        state = next_lcg(state);
        const int domain_id = static_cast<int>(state % static_cast<uint32_t>(domain_count));

        state = next_lcg(state);
        uint32_t topic_id = state % 5u;
        if ((state % 100u) < static_cast<uint32_t>(bias_ai)) {
            topic_id = 0u;
        }

        state = next_lcg(state);
        const unsigned score = static_cast<unsigned>(state % 1000u);
        const unsigned risk = static_cast<unsigned>((state >> 10) % 100u);
        const unsigned token = static_cast<unsigned>(state ^ (state >> 13));

        const int n = std::snprintf(
            g_output + pos,
            kOutputCap - pos - 2,
            "doc_%06d domain_%s topic_%s score_%u risk_%u token_%08x ",
            doc,
            domain_name(domain_id),
            topic_name(topic_id),
            score,
            risk,
            token);
        if (n <= 0) {
            break;
        }
        pos += n;
        ++doc;
    }

    if (pos > target_bytes) {
        pos = target_bytes;
    }
    if (pos >= kOutputCap - 2) {
        pos = kOutputCap - 3;
    }
    g_output[pos++] = '"';
    g_output[pos] = '\0';
    g_output_len = pos;
    return 0;
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
    long long values[4] = {1400000LL, 20260301LL, 3LL, 37LL};
    runtime_json::extract_ints(in, input_len, values, 4);

    const int target_bytes = static_cast<int>(runtime_json::extract_named_int(in, input_len, "target_bytes", values[0]));
    const uint32_t seed = static_cast<uint32_t>(runtime_json::extract_named_int(in, input_len, "seed", values[1]));
    const int domains = static_cast<int>(runtime_json::extract_named_int(in, input_len, "domains", values[2]));
    const int bias_ai = static_cast<int>(runtime_json::extract_named_int(in, input_len, "bias_ai", values[3]));
    return write_snapshot_json_string(target_bytes, seed, domains, bias_ai);
}

EMSCRIPTEN_KEEPALIVE int run(int a, int b) { return a + b; }
}
