#include <cstdint>
#include <cstdio>
#include <cstring>
#include <emscripten/emscripten.h>

#include "../common/runtime_browser.hpp"
#include "../common/runtime_json.hpp"

namespace {
constexpr int kInputCap = 16 * 1024;
constexpr int kURLCap = 1024;
constexpr int kFetchCap = 12 * 1024;
constexpr int kOutputCap = 3072;
constexpr const char* kFallbackURL =
    "https://en.wikipedia.org/w/api.php?action=query&format=json&origin=*&titles=Little%27s_law&prop=extracts&exintro=1&explaintext=1";

unsigned char g_input[kInputCap];
char g_url[kURLCap];
char g_fetch[kFetchCap];
char g_output[kOutputCap];
int g_output_len = 0;

int copy_url_or_fallback(const char* in, int input_len, char* out, int out_cap) {
    const char* url_ptr = nullptr;
    const int url_len = runtime_json::extract_named_string(in, input_len, "url", &url_ptr);
    if (!out || out_cap <= 0) {
        return 0;
    }
    if (url_ptr && url_len > 0) {
        const int copy = url_len < out_cap - 1 ? url_len : out_cap - 1;
        std::memcpy(out, url_ptr, static_cast<size_t>(copy));
        out[copy] = '\0';
        return copy;
    }
    const int len = static_cast<int>(std::strlen(kFallbackURL));
    const int copy = len < out_cap - 1 ? len : out_cap - 1;
    std::memcpy(out, kFallbackURL, static_cast<size_t>(copy));
    out[copy] = '\0';
    return copy;
}

bool ascii_equal_ci(char a, char b) {
    if (a >= 'A' && a <= 'Z') a = static_cast<char>(a - 'A' + 'a');
    if (b >= 'A' && b <= 'Z') b = static_cast<char>(b - 'A' + 'a');
    return a == b;
}

int count_substring_ci(const char* data, int len, const char* needle) {
    const int needle_len = static_cast<int>(std::strlen(needle));
    if (!data || len <= 0 || needle_len <= 0 || needle_len > len) {
        return 0;
    }
    int count = 0;
    for (int i = 0; i + needle_len <= len; ++i) {
        bool match = true;
        for (int j = 0; j < needle_len; ++j) {
            if (!ascii_equal_ci(data[i + j], needle[j])) {
                match = false;
                break;
            }
        }
        if (match) {
            ++count;
            i += needle_len - 1;
        }
    }
    return count;
}

int count_digits(const char* data, int len) {
    int count = 0;
    for (int i = 0; i < len; ++i) {
        if (data[i] >= '0' && data[i] <= '9') {
            ++count;
        }
    }
    return count;
}

int count_words(const char* data, int len) {
    int count = 0;
    bool in_word = false;
    for (int i = 0; i < len; ++i) {
        const unsigned char c = static_cast<unsigned char>(data[i]);
        const bool is_word = (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9');
        if (is_word && !in_word) {
            ++count;
        }
        in_word = is_word;
    }
    return count;
}

int make_preview(const char* data, int len, char* out, int out_cap, int max_chars) {
    int pos = 0;
    const int limit = len < max_chars ? len : max_chars;
    for (int i = 0; i < limit && pos < out_cap - 1; ++i) {
        const char c = data[i];
        if (c == '"' || c == '\\') {
            if (pos + 2 >= out_cap) break;
            out[pos++] = '\\';
            out[pos++] = c;
            continue;
        }
        if (c == '\n' || c == '\r' || c == '\t') {
            if (pos + 2 >= out_cap) break;
            out[pos++] = '\\';
            out[pos++] = (c == '\n') ? 'n' : (c == '\r' ? 'r' : 't');
            continue;
        }
        if (static_cast<unsigned char>(c) < 32) {
            continue;
        }
        out[pos++] = c;
    }
    out[pos] = '\0';
    return pos;
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
    copy_url_or_fallback(in, input_len, g_url, kURLCap);
    const int timeout_ms = static_cast<int>(runtime_json::extract_named_int(in, input_len, "timeout_ms", 3000));
    const runtime_browser::FetchResult response = runtime_browser::get(g_url, g_fetch, kFetchCap, timeout_ms);
    if (!response.ok) {
        g_output_len = std::snprintf(
            g_output,
            kOutputCap,
            "{\"ok\":false,\"reference_url\":\"%s\",\"http_status\":%d,\"error_code\":%d}",
            g_url,
            response.http_status,
            response.error_code);
        return g_output_len < 0 || g_output_len >= kOutputCap ? 2 : 0;
    }

    char preview[512];
    make_preview(response.data, response.bytes, preview, static_cast<int>(sizeof(preview)), 180);
    g_output_len = std::snprintf(
        g_output,
        kOutputCap,
        "{\"ok\":true,\"reference_url\":\"%s\",\"reference_bytes\":%d,\"reference_word_count\":%d,\"reference_digit_count\":%d,\"reference_law_mentions\":%d,\"reference_wait_mentions\":%d,\"preview\":\"%s\"}",
        g_url,
        response.bytes,
        count_words(response.data, response.bytes),
        count_digits(response.data, response.bytes),
        count_substring_ci(response.data, response.bytes, "law"),
        count_substring_ci(response.data, response.bytes, "wait"),
        preview);
    return g_output_len < 0 || g_output_len >= kOutputCap ? 3 : 0;
}
}
