#include "../common/workflow.hpp"

namespace {
constexpr int kArtifactCap = 16384;

int count_rows_after_header(const char* data, int len) {
    int rows = 0;
    bool saw_data_on_line = false;
    bool skip_header = true;
    for (int i = 0; i <= len; ++i) {
        const char c = i < len ? data[i] : '\n';
        if (skip_header) {
            if (c == '\n') {
                skip_header = false;
            }
            continue;
        }
        if (c != '\n' && c != '\r') {
            saw_data_on_line = true;
        }
        if (c == '\n') {
            if (saw_data_on_line) {
                ++rows;
            }
            saw_data_on_line = false;
        }
    }
    return rows;
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

WORKFLOW_NODE_WITH_CAPS(input, output, 8192, 1024) {
    const int timeout_ms = input.int_("timeout_ms", 3000);

    char body[kArtifactCap];
    const auto profile = input.artifact("load-profile").fetch_text(body, kArtifactCap, timeout_ms);
    const int rows = count_rows_after_header(profile.data, profile.bytes);
    const int total_arrivals = sum_csv_column(profile.data, profile.bytes, 1);
    const int total_service_ms = sum_csv_column(profile.data, profile.bytes, 2);

    auto json = output.object();
    json.field("profile_bytes", profile.bytes);
    json.field("profile_rows", rows);
    json.field("total_arrivals", total_arrivals);
    json.field("average_service_ms", rows > 0 ? total_service_ms / rows : 0);
    json.field("profile_checksum", ascii_sum(profile.data, profile.bytes));
    json.field("artifact_http_status", profile.http_status);
    json.done();
}
