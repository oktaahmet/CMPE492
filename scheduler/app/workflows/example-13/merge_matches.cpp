#include <cstdint>
#include <cstdio>
#include <cstring>
#include <fstream>
#include <string>

#include "../common/runtime_json.hpp"
#include "../common/runtime_node.hpp"

namespace {
int sum_named_ints(const char* data, int len, const char* key) {
    const int key_len = static_cast<int>(std::strlen(key));
    int sum = 0;
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
        if (!match || data[i + key_len + 1] != '"') {
            continue;
        }
        int k = i + key_len + 2;
        while (k < len && data[k] != ':') {
            ++k;
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
        sum += value;
    }
    return sum;
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
        if (!match || data[i + key_len + 1] != '"') {
            continue;
        }
        int k = i + key_len + 2;
        while (k < len && data[k] != ':') {
            ++k;
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

int next_named_string(const char* data, int len, const char* key, int start, const char** out) {
    const int key_len = static_cast<int>(std::strlen(key));
    for (int i = start; i + key_len + 3 < len; ++i) {
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
        if (!match || data[i + key_len + 1] != '"') {
            continue;
        }
        int k = i + key_len + 2;
        while (k < len && data[k] != ':') {
            ++k;
        }
        ++k;
        while (k < len && (data[k] == ' ' || data[k] == '\n' || data[k] == '\r' || data[k] == '\t')) {
            ++k;
        }
        if (k >= len || data[k] != '"') {
            continue;
        }
        ++k;
        *out = data + k;
        int n = 0;
        while (k + n < len && data[k + n] != '"' && data[k + n] != '\\') {
            ++n;
        }
        return n;
    }
    *out = nullptr;
    return 0;
}
}  // namespace

WORKFLOW_JSON_NODE(512 * 1024, 1024)

int workflow_run_json(const char* input, int input_len, char* output, int output_cap, int& output_len) {
    const char* path_ptr = nullptr;
    const int path_len = runtime_json::extract_named_string(input, input_len, "path", &path_ptr);
    if (path_len <= 0) {
        return 2;
    }

    const std::string path(path_ptr, static_cast<size_t>(path_len));
    std::ofstream report(path);
    if (!report) {
        return 3;
    }

    report << "index,input,hash\n";
    int written_matches = 0;
    int cursor = 0;
    const char* matches = nullptr;
    while (true) {
        const int matches_len = next_named_string(input, input_len, "matches", cursor, &matches);
        if (matches_len <= 0) {
            break;
        }
        cursor = static_cast<int>((matches - input) + matches_len);
        for (int i = 0; i < matches_len; ++i) {
            if (matches[i] == ';') {
                report << "\n";
                ++written_matches;
            } else {
                report << matches[i];
            }
        }
    }

    const int processed_total = sum_named_ints(input, input_len, "processed");
    const int shard_count = count_named_numbers(input, input_len, "range_start");
    const int reported_matches = sum_named_ints(input, input_len, "match_count");

    runtime_json::JsonWriter json(output, output_cap);
    json.begin_object();
    json.field("shard_count", shard_count);
    json.field("processed_total", processed_total);
    json.field("reported_matches", reported_matches);
    json.field("written_matches", written_matches);
    json.field("report", "matches.txt");
    output_len = json.end_object();
    return json.ok() ? 0 : 4;
}
