#include <cstdint>
#include <cstdio>
#include <cstring>
#include <emscripten/emscripten.h>

#include "../common/runtime_browser.hpp"
#include "../common/runtime_json.hpp"

namespace {
constexpr int kInputCap = 16 * 1024;
constexpr int kPageCap = 8 * 1024;
constexpr int kOutputCap = 2048;
constexpr const char* kTargetURL =
    "https://en.wikipedia.org/w/api.php?action=query&format=json&origin=*&titles=WebAssembly&prop=extracts&exintro=1&explaintext=1";

unsigned char g_input[kInputCap];
char g_page[kPageCap];
char g_output[kOutputCap];
int g_output_len = 0;

int count_char(const char* data, int len, char needle) {
    if (!data || len <= 0) {
        return 0;
    }
    int count = 0;
    for (int i = 0; i < len; ++i) {
        if (data[i] == needle) {
            ++count;
        }
    }
    return count;
}

int count_word_like_tokens(const char* data, int len) {
    if (!data || len <= 0) {
        return 0;
    }
    int count = 0;
    bool in_word = false;
    for (int i = 0; i < len; ++i) {
        const unsigned char c = static_cast<unsigned char>(data[i]);
        const bool is_word = (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9');
        if (is_word && !in_word) {
            ++count;
        }
        in_word = is_word;
    }
    return count;
}

int count_substring(const char* data, int len, const char* needle) {
    if (!data || len <= 0 || !needle) {
        return 0;
    }
    const int needle_len = static_cast<int>(std::strlen(needle));
    if (needle_len <= 0 || needle_len > len) {
        return 0;
    }
    int count = 0;
    for (int i = 0; i + needle_len <= len; ++i) {
        bool match = true;
        for (int j = 0; j < needle_len; ++j) {
            if (data[i + j] != needle[j]) {
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

int make_json_preview(const char* data, int len, char* out, int out_cap, int max_input_chars) {
    if (!data || len <= 0 || !out || out_cap <= 0) {
        if (out && out_cap > 0) {
            out[0] = '\0';
        }
        return 0;
    }
    int pos = 0;
    const int max_chars = len < max_input_chars ? len : max_input_chars;
    for (int i = 0; i < max_chars && pos < out_cap - 1; ++i) {
        const char c = data[i];
        if (c == '"' || c == '\\') {
            if (pos + 2 >= out_cap) break;
            out[pos++] = '\\';
            out[pos++] = c;
            continue;
        }
        if (c == '\n') {
            if (pos + 2 >= out_cap) break;
            out[pos++] = '\\';
            out[pos++] = 'n';
            continue;
        }
        if (c == '\r') {
            if (pos + 2 >= out_cap) break;
            out[pos++] = '\\';
            out[pos++] = 'r';
            continue;
        }
        if (c == '\t') {
            if (pos + 2 >= out_cap) break;
            out[pos++] = '\\';
            out[pos++] = 't';
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
    const int timeout_ms = static_cast<int>(runtime_json::extract_named_int(in, input_len, "timeout_ms", 3000));
    const runtime_browser::FetchResult response = runtime_browser::get(kTargetURL, g_page, kPageCap, timeout_ms);

    if (!response.ok) {
      g_output_len = std::snprintf(
          g_output,
          kOutputCap,
          "{\"ok\":false,\"url\":\"%s\",\"http_status\":%d,\"error_code\":%d}",
          kTargetURL,
          response.http_status,
          response.error_code);
      if (g_output_len < 0 || g_output_len >= kOutputCap) {
          g_output_len = 0;
          return 2;
      }
      return 0;
    }

    const int a_count = count_char(response.data, response.bytes, 'a');
    const int word_count = count_word_like_tokens(response.data, response.bytes);
    const int webassembly_mentions = count_substring(response.data, response.bytes, "WebAssembly");
    char preview[480];
    make_json_preview(response.data, response.bytes, preview, static_cast<int>(sizeof(preview)), 220);
    g_output_len = std::snprintf(
        g_output,
        kOutputCap,
        "{\"ok\":true,\"url\":\"%s\",\"bytes\":%d,\"count_a\":%d,\"word_count\":%d,\"webassembly_mentions\":%d,\"http_status\":%d,\"preview\":\"%s\"}",
        kTargetURL,
        response.bytes,
        a_count,
        word_count,
        webassembly_mentions,
        response.http_status,
        preview);
    if (g_output_len < 0 || g_output_len >= kOutputCap) {
        g_output_len = 0;
        return 3;
    }
    return 0;
}
}
