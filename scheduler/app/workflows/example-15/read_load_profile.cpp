#include <cstdio>

#include "../common/runtime_artifacts.hpp"
#include "../common/runtime_json.hpp"
#include "../common/runtime_node.hpp"

// Node: read-load-profile
//
// This browser-worker node shows how workflow input artifacts are meant to be
// used: the workflow declares a file once, the node opts in with
// uses_artifacts, and the runtime injects a fetchable URL into the input
// context.
//
// Authoring note:
// use artifacts for real files or datasets, not for tiny configuration values.
// Small knobs belong in args; artifacts are for inputs that should live beside
// the workflow on disk.

namespace {
constexpr int kArtifactCap = 16384;

int count_lines(const char* data, int len) {
    int lines = len > 0 ? 1 : 0;
    for (int i = 0; i < len; ++i) {
        if (data[i] == '\n') {
            ++lines;
        }
    }
    return lines;
}

int ascii_sum(const char* data, int len) {
    int sum = 0;
    for (int i = 0; i < len; ++i) {
        sum = (sum + static_cast<unsigned char>(data[i])) % 100000;
    }
    return sum;
}

int sum_csv_column(const char* data, int len, int column) {
    int sum = 0;
    int current_column = 0;
    int value = 0;
    bool in_number = false;
    bool skip_header = true;
    // This workflow only needs a simple numeric scan. A full CSV parser would
    // be overkill for an example whose purpose is artifact wiring and basic
    // summarization.
    for (int i = 0; i <= len; ++i) {
        const char c = i < len ? data[i] : '\n';
        if (skip_header) {
            if (c == '\n') {
                skip_header = false;
                current_column = 0;
            }
            continue;
        }
        if (c >= '0' && c <= '9') {
            if (current_column == column) {
                value = value * 10 + static_cast<int>(c - '0');
                in_number = true;
            }
            continue;
        }
        if (c == ',' || c == '\n' || c == '\r') {
            if (current_column == column && in_number) {
                sum += value;
            }
            value = 0;
            in_number = false;
            if (c == ',') {
                ++current_column;
            } else if (c == '\n') {
                current_column = 0;
            }
        }
    }
    return sum;
}
}  // namespace

WORKFLOW_JSON_NODE(8192, 1024)

int workflow_run_json(const char* input, int input_len, char* output, int output_cap, int& output_len) {
    const int timeout_ms = static_cast<int>(runtime_json::extract_named_int(input, input_len, "timeout_ms", 3000));
    char body[kArtifactCap];
    char url[1024];
    // The artifact id is explicit here. If the workflow adds more input files,
    // this node still fetches only the one it asked for.
    const runtime_artifacts::TextArtifact profile =
        runtime_artifacts::fetch_text(input, input_len, "load-profile", body, kArtifactCap, timeout_ms, url, 1024);
    if (!profile.ok) {
        output_len = runtime_artifacts::write_fetch_error(output, output_cap, profile);
        return output_len > 0 ? 0 : 2;
    }

    const int lines = count_lines(profile.data, profile.bytes);
    const int rows = lines > 0 ? lines - 1 : 0;
    const int total_arrivals = sum_csv_column(profile.data, profile.bytes, 1);
    const int total_service_ms = sum_csv_column(profile.data, profile.bytes, 2);

    // Downstream simulation nodes consume this compact summary instead of the
    // whole CSV. That keeps browser results small and decouples later nodes
    // from the raw file layout.
    runtime_json::JsonWriter json(output, output_cap);
    json.begin_object();
    json.field("profile_bytes", profile.bytes);
    json.field("profile_rows", rows);
    json.field("total_arrivals", total_arrivals);
    json.field("average_service_ms", rows > 0 ? total_service_ms / rows : 0);
    json.field("profile_checksum", ascii_sum(profile.data, profile.bytes));
    json.field("artifact_http_status", profile.http_status);
    output_len = json.end_object();
    return json.ok() ? 0 : 3;
}
