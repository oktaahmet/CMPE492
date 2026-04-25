#include <cstdio>
#include <cstring>
#include <fstream>
#include <string>

#include "../common/runtime_json.hpp"
#include "../common/runtime_node.hpp"

// Node: aggregate-simulations
//
// This server-side reducer consumes several finalized dependency outputs:
// browser fetch summaries, collect_all simulation samples, and the large trace
// text payload. It then writes both a compact JSON summary and a text artifact.
//
// Authoring note:
// reducers are a good place for file generation. The scheduler stores the small
// JSON output in PostgreSQL, while larger generated files can stay on disk as
// output artifacts.

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
        if (match && data[i + key_len + 1] == '"') {
            ++count;
        }
    }
    return count;
}

int count_substring(const char* data, int len, const char* needle) {
    const int needle_len = static_cast<int>(std::strlen(needle));
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
        }
    }
    return count;
}

int extract_artifact_path(const char* data, int len, const char* artifact_id, const char** out) {
    const int id_len = static_cast<int>(std::strlen(artifact_id));
    // The input context can contain several path fields from different
    // artifacts. Always look up the path by artifact id instead of taking the
    // first "path" key you find.
    for (int i = 0; i + id_len + 2 < len; ++i) {
        if (data[i] != '"') {
            continue;
        }
        bool id_match = true;
        for (int j = 0; j < id_len; ++j) {
            if (data[i + 1 + j] != artifact_id[j]) {
                id_match = false;
                break;
            }
        }
        if (!id_match || data[i + id_len + 1] != '"') {
            continue;
        }
        for (int k = i + id_len + 2; k + 8 < len && data[k] != '}'; ++k) {
            if (std::strncmp(data + k, "\"path\"", 6) != 0) {
                continue;
            }
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
    }
    *out = nullptr;
    return 0;
}
}  // namespace

WORKFLOW_JSON_NODE(1024 * 1024, 2048)

int workflow_run_json(const char* input, int input_len, char* output, int output_cap, int& output_len) {
    const char* path_ptr = nullptr;
    const int path_len = extract_artifact_path(input, input_len, "simulation-report", &path_ptr);
    if (path_len <= 0) {
        return 2;
    }
    const std::string report_path(path_ptr, static_cast<size_t>(path_len));

    const int wait_count = count_named_numbers(input, input_len, "avg_wait_ms");
    const int avg_wait_ms = wait_count > 0 ? sum_named_ints(input, input_len, "avg_wait_ms") / wait_count : 0;
    const int avg_system_ms = wait_count > 0 ? sum_named_ints(input, input_len, "avg_system_ms") / wait_count : 0;
    const int max_wait_ms = sum_named_ints(input, input_len, "max_wait_ms");
    const int served_total = sum_named_ints(input, input_len, "served_customers");
    const int events_total = sum_named_ints(input, input_len, "total_customer_events");
    const int source_bytes = sum_named_ints(input, input_len, "source_bytes");
    const int profile_rows = sum_named_ints(input, input_len, "profile_rows");
    const int profile_bytes = sum_named_ints(input, input_len, "profile_bytes");
    const int trace_samples = count_substring(input, input_len, "trace_id=");

    // Write a durable text artifact alongside the normal JSON output. The
    // runtime will later hash, size, and expose this file through the artifact
    // endpoint.
    std::ofstream report(report_path);
    if (!report) {
        return 3;
    }
    report << "workflow=wf-full-system-capability-015\n";
    report << "simulation_samples=" << wait_count << "\n";
    report << "trace_samples=" << trace_samples << "\n";
    report << "avg_wait_ms=" << avg_wait_ms << "\n";
    report << "avg_system_ms=" << avg_system_ms << "\n";
    report << "max_wait_sum=" << max_wait_ms << "\n";
    report << "served_total=" << served_total << "\n";
    report << "events_total=" << events_total << "\n";
    report << "source_bytes=" << source_bytes << "\n";
    report << "profile_rows=" << profile_rows << "\n";
    report << "profile_bytes=" << profile_bytes << "\n";

    // Return the compact reducer summary that downstream nodes actually need.
    runtime_json::JsonWriter json(output, output_cap);
    json.begin_object();
    json.field("simulation_sample_count", wait_count);
    json.field("trace_sample_count", trace_samples);
    json.field("average_wait_ms", avg_wait_ms);
    json.field("average_system_ms", avg_system_ms);
    json.field("served_total", served_total);
    json.field("events_total", events_total);
    json.field("source_bytes", source_bytes);
    json.field("profile_rows", profile_rows);
    json.field("profile_bytes", profile_bytes);
    json.field("report", "simulation_report.txt");
    output_len = json.end_object();
    return json.ok() ? 0 : 4;
}
