#include <cstring>

#include "../common/runtime_json.hpp"
#include "../common/runtime_node.hpp"

namespace {
int sum_named_ints(const char* data, int len, const char* key) {
    const int key_len = static_cast<int>(std::strlen(key));
    int total = 0;
    for (int i = 0; i + key_len + 3 < len; ++i) {
        if (data[i] != '"') {
            continue;
        }
        bool match = true;
        for (int j = 0; j < key_len; ++j) {
            if (data[i + 1 + j] != key[j]) {
                match = false;
                break;
            }
        }
        if (!match || data[i + 1 + key_len] != '"') {
            continue;
        }
        int k = i + key_len + 2;
        while (k < len && data[k] != ':') {
            ++k;
        }
        if (k >= len) {
            continue;
        }
        ++k;
        while (k < len && (data[k] == ' ' || data[k] == '\n' || data[k] == '\r' || data[k] == '\t')) {
            ++k;
        }
        int value = 0;
        while (k < len && data[k] >= '0' && data[k] <= '9') {
            value = value * 10 + static_cast<int>(data[k] - '0');
            ++k;
        }
        total += value;
    }
    return total;
}

int count_named_numbers(const char* data, int len, const char* key) {
    const int key_len = static_cast<int>(std::strlen(key));
    int count = 0;
    for (int i = 0; i + key_len + 3 < len; ++i) {
        if (data[i] != '"') {
            continue;
        }
        bool match = true;
        for (int j = 0; j < key_len; ++j) {
            if (data[i + 1 + j] != key[j]) {
                match = false;
                break;
            }
        }
        if (!match || data[i + 1 + key_len] != '"') {
            continue;
        }
        int k = i + key_len + 2;
        while (k < len && data[k] != ':') {
            ++k;
        }
        if (k >= len) {
            continue;
        }
        ++k;
        while (k < len && (data[k] == ' ' || data[k] == '\n' || data[k] == '\r' || data[k] == '\t')) {
            ++k;
        }
        if (k < len && data[k] >= '0' && data[k] <= '9') {
            ++count;
        }
    }
    return count;
}
}  // namespace

WORKFLOW_JSON_NODE(64 * 1024, 1024)

int workflow_run_json(const char* input, int input_len, char* output, int output_cap, int& output_len) {
    const int expected_score_samples = static_cast<int>(runtime_json::extract_named_int(input, input_len, "expected_score_samples", 3));
    const int score_count = count_named_numbers(input, input_len, "score");
    const int score_total = sum_named_ints(input, input_len, "score");
    const int score_average = score_count > 0 ? score_total / score_count : 0;
    const int source_sample_count = static_cast<int>(runtime_json::extract_named_int(input, input_len, "sample_count", 0));

    runtime_json::JsonWriter json(output, output_cap);
    json.begin_object();
    json.field("source_sample_count", source_sample_count);
    json.field("score_sample_count", score_count);
    json.field("expected_score_samples", expected_score_samples);
    json.field("score_average", score_average);
    json.field("complete", score_count == expected_score_samples);
    json.field("review", score_average >= 760 ? "manual_review" : "auto_accept");
    output_len = json.end_object();
    return json.ok() ? 0 : 2;
}
