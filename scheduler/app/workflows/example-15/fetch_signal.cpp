#include <cstring>

#include "../common/workflow.hpp"

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

WORKFLOW_NODE_WITH_CAPS(input, output, 8192, 2048) {
    const auto url = input.string("url");
    const int timeout_ms = input.int_("timeout_ms", 3000);

    char body[kFetchCap];
    const auto fetched = workflow::fetch_text(url, body, kFetchCap, timeout_ms);

    auto json = output.object();
    json.field("ok", fetched.ok);
    json.field("source_bytes", fetched.bytes);
    json.field("source_words", count_wordish(body, fetched.bytes));
    json.field("simulation_mentions", count_substring(body, fetched.bytes, "simulation"));
    json.field("queue_mentions", count_substring(body, fetched.bytes, "queue"));
    json.field("source_checksum", static_cast<int>(checksum(body, fetched.bytes) % 100000));
    json.done();
}
