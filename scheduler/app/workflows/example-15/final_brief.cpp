#include <cstdio>
#include <cstring>
#include <fstream>
#include <string>

#include "../common/runtime_json.hpp"
#include "../common/runtime_node.hpp"

// Node: final-brief
//
// This final server-side node combines the aggregate reducer output with the
// primitive score node, writes the last text artifact, and returns a compact
// final summary object.
//
// Authoring note:
// final nodes are often where you translate machine-oriented intermediate
// values into a presentation-friendly artifact that a human can download or
// inspect.

namespace {
int extract_node_output_number(const char* data, int len, const char* node_id, int fallback) {
    const int node_len = static_cast<int>(std::strlen(node_id));
    for (int i = 0; i + node_len + 2 < len; ++i) {
        if (data[i] != '"') {
            continue;
        }
        bool node_match = true;
        for (int j = 0; j < node_len; ++j) {
            if (data[i + 1 + j] != node_id[j]) {
                node_match = false;
                break;
            }
        }
        if (!node_match || data[i + node_len + 1] != '"') {
            continue;
        }
        for (int k = i + node_len + 2; k + 9 < len; ++k) {
            if (data[k] == '}' || data[k] == ']') {
                break;
            }
            if (std::strncmp(data + k, "\"output\"", 8) != 0) {
                continue;
            }
            while (k < len && data[k] != ':') {
                ++k;
            }
            ++k;
            while (k < len && (data[k] == ' ' || data[k] == '\n' || data[k] == '\r' || data[k] == '\t')) {
                ++k;
            }
            int value = 0;
            bool found = false;
            while (k < len && data[k] >= '0' && data[k] <= '9') {
                value = value * 10 + static_cast<int>(data[k] - '0');
                found = true;
                ++k;
            }
            return found ? value : fallback;
        }
    }
    return fallback;
}

int extract_artifact_path(const char* data, int len, const char* artifact_id, const char** out) {
    const int id_len = static_cast<int>(std::strlen(artifact_id));
    // Just like the earlier reducer, this node must look up its own output
    // artifact path by id. Dependency artifacts can introduce other path fields
    // into the same input JSON.
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

WORKFLOW_JSON_NODE(131072, 1536)

int workflow_run_json(const char* input, int input_len, char* output, int output_cap, int& output_len) {
    const char* path_ptr = nullptr;
    const int path_len = extract_artifact_path(input, input_len, "final-brief", &path_ptr);
    if (path_len <= 0) {
        return 2;
    }
    const std::string brief_path(path_ptr, static_cast<size_t>(path_len));

    const int average_wait_ms = static_cast<int>(runtime_json::extract_named_int(input, input_len, "average_wait_ms", 0));
    const int simulation_sample_count =
        static_cast<int>(runtime_json::extract_named_int(input, input_len, "simulation_sample_count", 0));
    const int trace_sample_count = static_cast<int>(runtime_json::extract_named_int(input, input_len, "trace_sample_count", 0));
    // score-policy returned a primitive number, so we read it from the specific
    // dependency node rather than treating every upstream output as an object.
    const int score = extract_node_output_number(input, input_len, "score-policy", 0);

    std::ofstream brief(brief_path);
    if (!brief) {
        return 3;
    }
    brief << "Full system workflow capability run\n";
    brief << "simulation_sample_count=" << simulation_sample_count << "\n";
    brief << "trace_sample_count=" << trace_sample_count << "\n";
    brief << "average_wait_ms=" << average_wait_ms << "\n";
    brief << "policy_score=" << score << "\n";
    brief << "notes=browser fetch, artifact fetch, collect_all, consensus, server reducer, large output, and output artifacts all ran in one DAG\n";

    // Return a small JSON summary beside the text artifact so the UI can show
    // a useful result without downloading the file first.
    runtime_json::JsonWriter json(output, output_cap);
    json.begin_object();
    json.field("simulation_sample_count", simulation_sample_count);
    json.field("trace_sample_count", trace_sample_count);
    json.field("average_wait_ms", average_wait_ms);
    json.field("policy_score", score);
    json.field("brief", "final_brief.txt");
    output_len = json.end_object();
    return json.ok() ? 0 : 4;
}
