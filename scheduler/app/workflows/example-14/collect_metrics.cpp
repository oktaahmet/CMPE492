#include "../common/runtime_json.hpp"
#include "../common/runtime_node.hpp"

namespace {
int clamp_int(long long value, int min_value, int max_value) {
    if (value < min_value) return min_value;
    if (value > max_value) return max_value;
    return static_cast<int>(value);
}
}  // namespace

WORKFLOW_JSON_NODE(4096, 1024)

int workflow_run_json(const char* input, int input_len, char* output, int output_cap, int& output_len) {
    const int seed = clamp_int(runtime_json::extract_named_int(input, input_len, "seed", 42), 1, 1000000);
    const int samples = clamp_int(runtime_json::extract_named_int(input, input_len, "samples", 18), 4, 64);

    int min_value = 1000000;
    int max_value = -1000000;
    int total = 0;
    int checksum = 0;

    for (int i = 0; i < samples; ++i) {
        const int value = (seed * 37 + i * 53 + (i % 5) * 29) % 1000;
        if (value < min_value) min_value = value;
        if (value > max_value) max_value = value;
        total += value;
        checksum = (checksum + value * (i + 1)) % 100000;
    }

    runtime_json::JsonWriter json(output, output_cap);
    json.begin_object();
    json.field("sample_count", samples);
    json.field("min_value", min_value);
    json.field("max_value", max_value);
    json.field("avg_value", total / samples);
    json.field("checksum", checksum);
    output_len = json.end_object();
    return json.ok() ? 0 : 2;
}
