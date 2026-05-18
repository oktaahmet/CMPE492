#include <cstdio>
#include <cstring>

#include "../common/workflow.hpp"

namespace {
constexpr int kPageCap = 8 * 1024;
constexpr const char* kTargetURL =
    "https://en.wikipedia.org/w/api.php?action=query&format=json&origin=*&titles=WebAssembly&prop=extracts&exintro=1&explaintext=1";

char g_page[kPageCap];

int count_char(const char* data, int len, char needle) {
    int count = 0;
    for (int i = 0; data && i < len; ++i) {
        if (data[i] == needle) {
            ++count;
        }
    }
    return count;
}

int count_word_like_tokens(const char* data, int len) {
    int count = 0;
    bool in_word = false;
    for (int i = 0; data && i < len; ++i) {
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
    const int needle_len = static_cast<int>(std::strlen(needle));
    if (!data || needle_len <= 0 || needle_len > len) {
        return 0;
    }
    int count = 0;
    for (int i = 0; i + needle_len <= len; ++i) {
        if (std::memcmp(data + i, needle, static_cast<size_t>(needle_len)) == 0) {
            ++count;
            i += needle_len - 1;
        }
    }
    return count;
}

void make_json_preview(const char* data, int len, char* out, int out_cap, int max_input_chars) {
    int pos = 0;
    const int max_chars = len < max_input_chars ? len : max_input_chars;
    for (int i = 0; data && i < max_chars && pos < out_cap - 1; ++i) {
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

// WORKFLOW_NODE_WITH_CAPS(input, output, IN_CAP, OUT_CAP): the input cap
// holds the runtime input context (args + dependency outputs + artifact
// descriptors). 16 KiB leaves room here even though this node only reads
// `args.timeout_ms`. OUT_CAP must fit the full JSON object we build below.
WORKFLOW_NODE_WITH_CAPS(input, output, 16 * 1024, 2048) {
    const int timeout_ms = input.int_("timeout_ms", 3000);
    // runtime_browser::get does a GET via the host runtime's fetchx_text
    // import. Limits: GET only, http/https only, timeout cap 10 000 ms,
    // response cap 16 MB, CORS rules apply. See HTTP_GET_GUIDE.md.
    const runtime_browser::FetchResult response = runtime_browser::get(kTargetURL, g_page, kPageCap, timeout_ms);

    char preview[480] = {};
    if (response.ok) {
        make_json_preview(response.data, response.bytes, preview, static_cast<int>(sizeof(preview)), 220);
    }

    auto json = output.object();
    json.field("ok", response.ok);
    json.field("url", kTargetURL);
    json.field("http_status", response.http_status);
    if (!response.ok) {
        json.field("error_code", response.error_code);
        json.done();
        return;
    }
    json.field("bytes", response.bytes);
    json.field("count_a", count_char(response.data, response.bytes, 'a'));
    json.field("word_count", count_word_like_tokens(response.data, response.bytes));
    json.field("webassembly_mentions", count_substring(response.data, response.bytes, "WebAssembly"));
    json.field("preview", preview);
    json.done();
}
