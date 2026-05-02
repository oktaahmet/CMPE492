#include <cstdio>
#include <cstring>

#include "../common/workflow.hpp"

namespace {
constexpr int kURLCap = 1024;
constexpr int kFetchCap = 12 * 1024;
constexpr const char* kFallbackURL =
    "https://en.wikipedia.org/w/api.php?action=query&format=json&origin=*&titles=Little%27s_law&prop=extracts&exintro=1&explaintext=1";

char g_url[kURLCap];
char g_fetch[kFetchCap];

void copy_url(std::string_view url) {
    if (url.empty()) {
        url = kFallbackURL;
    }
    const size_t copy = url.size() < static_cast<size_t>(kURLCap - 1) ? url.size() : static_cast<size_t>(kURLCap - 1);
    std::memcpy(g_url, url.data(), copy);
    g_url[copy] = '\0';
}

bool ascii_equal_ci(char a, char b) {
    if (a >= 'A' && a <= 'Z') a = static_cast<char>(a - 'A' + 'a');
    if (b >= 'A' && b <= 'Z') b = static_cast<char>(b - 'A' + 'a');
    return a == b;
}

int count_substring_ci(const char* data, int len, const char* needle) {
    const int needle_len = static_cast<int>(std::strlen(needle));
    if (!data || needle_len <= 0 || needle_len > len) {
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
    for (int i = 0; data && i < len; ++i) {
        if (data[i] >= '0' && data[i] <= '9') {
            ++count;
        }
    }
    return count;
}

int count_words(const char* data, int len) {
    int count = 0;
    bool in_word = false;
    for (int i = 0; data && i < len; ++i) {
        const unsigned char c = static_cast<unsigned char>(data[i]);
        const bool is_word = (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9');
        if (is_word && !in_word) {
            ++count;
        }
        in_word = is_word;
    }
    return count;
}

void make_preview(const char* data, int len, char* out, int out_cap, int max_chars) {
    int pos = 0;
    const int limit = len < max_chars ? len : max_chars;
    for (int i = 0; data && i < limit && pos < out_cap - 1; ++i) {
        const char c = data[i];
        if (c == '"' || c == '\\') {
            if (pos + 2 >= out_cap) break;
            out[pos++] = '\\';
            out[pos++] = c;
        } else if (c == '\n' || c == '\r' || c == '\t') {
            if (pos + 2 >= out_cap) break;
            out[pos++] = '\\';
            out[pos++] = c == '\n' ? 'n' : (c == '\r' ? 'r' : 't');
        } else if (static_cast<unsigned char>(c) >= 32) {
            out[pos++] = c;
        }
    }
    out[pos] = '\0';
}
}  // namespace

WORKFLOW_NODE_WITH_CAPS(input, output, 16 * 1024, 3072) {
    copy_url(input.string("url"));
    const int timeout_ms = input.int_("timeout_ms", 3000);
    const runtime_browser::FetchResult response = runtime_browser::get(g_url, g_fetch, kFetchCap, timeout_ms);

    auto json = output.object();
    json.field("ok", response.ok);
    json.field("reference_url", g_url);
    json.field("http_status", response.http_status);
    if (!response.ok) {
        json.field("error_code", response.error_code);
        json.done();
        return;
    }

    char preview[512] = {};
    make_preview(response.data, response.bytes, preview, static_cast<int>(sizeof(preview)), 180);
    json.field("reference_bytes", response.bytes);
    json.field("reference_word_count", count_words(response.data, response.bytes));
    json.field("reference_digit_count", count_digits(response.data, response.bytes));
    json.field("reference_law_mentions", count_substring_ci(response.data, response.bytes, "law"));
    json.field("reference_wait_mentions", count_substring_ci(response.data, response.bytes, "wait"));
    json.field("preview", preview);
    json.done();
}
