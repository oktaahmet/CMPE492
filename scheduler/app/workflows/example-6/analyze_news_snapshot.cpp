#include <cstdint>
#include <cstdio>
#include <emscripten/emscripten.h>

#include "../common/runtime_json.hpp"

namespace {
constexpr int kInputCap = 12 * 1024 * 1024;
constexpr int kOutputCap = 2048;

unsigned char g_input[kInputCap];
char g_output[kOutputCap];
int g_output_len = 0;

int count_token(const char* data, int len, const char* token) {
    const int tlen = static_cast<int>(std::strlen(token));
    if (tlen <= 0 || len < tlen) {
        return 0;
    }

    int count = 0;
    for (int i = 0; i + tlen <= len; ++i) {
        bool match = true;
        for (int j = 0; j < tlen; ++j) {
            if (data[i + j] != token[j]) {
                match = false;
                break;
            }
        }
        if (match) {
            ++count;
            i += tlen - 1;
        }
    }
    return count;
}

uint64_t rolling_digest(const char* data, int len, int passes, int window) {
    if (passes < 1) {
        passes = 1;
    }
    if (window < 4) {
        window = 4;
    }

    uint64_t acc = 0x9e3779b97f4a7c15ULL;
    for (int p = 0; p < passes; ++p) {
        for (int i = 0; i < len; ++i) {
            const uint64_t c = static_cast<unsigned char>(data[i]);
            const uint64_t mix = c + static_cast<uint64_t>((i % window) + 1) * 1315423911ULL;
            acc ^= mix + (acc << 7) + (acc >> 3) + static_cast<uint64_t>(p + 1);
        }
    }
    return acc;
}

int write_summary_json_string(
    int text_len,
    int docs,
    int ai_hits,
    int macro_hits,
    int sec_hits,
    int tech_docs,
    int finance_docs,
    int science_docs,
    uint64_t digest) {
    g_output_len = std::snprintf(
        g_output,
        kOutputCap,
        "\"summary bytes=%d docs=%d topic_ai=%d topic_macro=%d topic_security=%d domain_tech=%d domain_finance=%d domain_science=%d digest=%016llx\"",
        text_len,
        docs,
        ai_hits,
        macro_hits,
        sec_hits,
        tech_docs,
        finance_docs,
        science_docs,
        static_cast<unsigned long long>(digest));
    if (g_output_len < 0 || g_output_len >= kOutputCap) {
        g_output_len = 0;
        return 2;
    }
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
    long long values[2] = {2LL, 211LL};
    runtime_json::extract_ints(in, input_len, values, 2);
    int passes = static_cast<int>(runtime_json::extract_named_int(in, input_len, "passes", values[0]));
    int window = static_cast<int>(runtime_json::extract_named_int(in, input_len, "window", values[1]));
    if (passes <= 0) {
        passes = 2;
    }
    if (window <= 0) {
        window = 211;
    }

    const char* upstream = nullptr;
    const int text_len = runtime_json::extract_input_output_string(
        in,
        input_len,
        "crawl-news-snapshot",
        &upstream);
    if (!upstream || text_len <= 0) {
        return write_summary_json_string(0, 0, 0, 0, 0, 0, 0, 0, 0ULL);
    }

    const int docs = count_token(upstream, text_len, "doc_");
    const int ai_hits = count_token(upstream, text_len, "topic_ai");
    const int macro_hits = count_token(upstream, text_len, "topic_macro");
    const int sec_hits = count_token(upstream, text_len, "topic_security");
    const int tech_docs = count_token(upstream, text_len, "domain_technews");
    const int finance_docs = count_token(upstream, text_len, "domain_financedaily");
    const int science_docs = count_token(upstream, text_len, "domain_sciencewire");
    const uint64_t digest = rolling_digest(upstream, text_len, passes, window);

    return write_summary_json_string(
        text_len,
        docs,
        ai_hits,
        macro_hits,
        sec_hits,
        tech_docs,
        finance_docs,
        science_docs,
        digest);
}

EMSCRIPTEN_KEEPALIVE int run(int a, int b) { return a * b; }
}
