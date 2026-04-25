#include <cstdio>
#include <cstring>

#include "../common/runtime_browser.hpp"
#include "../common/runtime_json.hpp"
#include "../common/runtime_node.hpp"

// Node: fetch-public-signal
//
// This browser-worker node demonstrates the smallest useful HTTP fetch pattern:
// read a URL from workflow args, fetch a text response through the browser host
// import, and return a compact JSON object for downstream nodes.
//
// Authoring note:
// downstream reducers should usually consume small summaries like this instead
// of a full remote document. That keeps result payloads stable and makes
// consensus easier because workers compare the same derived fields instead of a
// larger, noisier raw body.

namespace {
constexpr int kFetchCap = 32768;

int count_wordish(const char* data, int len) {
    int count = 0;
    bool in_word = false;
    for (int i = 0; i < len; ++i) {
        const bool word = (data[i] >= 'A' && data[i] <= 'Z') || (data[i] >= 'a' && data[i] <= 'z');
        if (word && !in_word) {
            ++count;
        }
        in_word = word;
    }
    return count;
}

int count_substring(const char* data, int len, const char* needle) {
    const int needle_len = static_cast<int>(std::strlen(needle));
    int count = 0;
    for (int i = 0; i + needle_len <= len; ++i) {
        bool ok = true;
        for (int j = 0; j < needle_len; ++j) {
            char a = data[i + j];
            char b = needle[j];
            if (a >= 'A' && a <= 'Z') a = static_cast<char>(a - 'A' + 'a');
            if (b >= 'A' && b <= 'Z') b = static_cast<char>(b - 'A' + 'a');
            if (a != b) {
                ok = false;
                break;
            }
        }
        if (ok) {
            ++count;
        }
    }
    return count;
}

unsigned checksum(const char* data, int len) {
    unsigned h = 2166136261u;
    for (int i = 0; i < len; ++i) {
        h ^= static_cast<unsigned char>(data[i]);
        h *= 16777619u;
    }
    return h;
}
}  // namespace

WORKFLOW_JSON_NODE(8192, 2048)

int workflow_run_json(const char* input, int input_len, char* output, int output_cap, int& output_len) {
    // The URL is passed through args in the workflow JSON. This node only owns
    // fetch + lightweight analysis; workflow structure decides where the URL
    // comes from.
    const char* url_ptr = nullptr;
    const int url_len = runtime_json::extract_named_string(input, input_len, "url", &url_ptr);
    if (url_len <= 0 || url_len >= 1024) {
        return 2;
    }

    char url[1024];
    std::memcpy(url, url_ptr, static_cast<size_t>(url_len));
    url[url_len] = '\0';

    const int timeout_ms = static_cast<int>(runtime_json::extract_named_int(input, input_len, "timeout_ms", 3000));
    char body[kFetchCap];
    const runtime_browser::FetchResult fetched = runtime_browser::get(url, body, kFetchCap, timeout_ms);
    if (!fetched.ok) {
        // Returning a structured error object is often more useful than failing
        // the whole workflow blindly: reducers and UIs can still inspect the
        // HTTP status / fetch error code.
        output_len = std::snprintf(
            output,
            output_cap,
            "{\"ok\":false,\"http_status\":%d,\"error_code\":%d}",
            fetched.http_status,
            fetched.error_code);
        return output_len < 0 || output_len >= output_cap ? 3 : 0;
    }

    // Return a small object rather than the raw body. In real workflows, this
    // is the common pattern: fetch externally, extract the few facts that the
    // DAG actually needs, and keep the rest out of the scheduler payload path.
    runtime_json::JsonWriter json(output, output_cap);
    json.begin_object();
    json.field("ok", true);
    json.field("source_bytes", fetched.bytes);
    json.field("source_words", count_wordish(body, fetched.bytes));
    json.field("simulation_mentions", count_substring(body, fetched.bytes, "simulation"));
    json.field("queue_mentions", count_substring(body, fetched.bytes, "queue"));
    json.field("source_checksum", static_cast<int>(checksum(body, fetched.bytes) % 100000));
    output_len = json.end_object();
    return json.ok() ? 0 : 4;
}
