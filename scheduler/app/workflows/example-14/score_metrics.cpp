#include "../common/runtime_json.hpp"
#include "../common/runtime_node.hpp"

namespace {
const char* risk_label(int score, int warning_threshold) {
    if (score >= warning_threshold + 180) return "critical";
    if (score >= warning_threshold) return "watch";
    return "normal";
}
}  // namespace

WORKFLOW_JSON_NODE(8192, 1024)

int workflow_run_json(const char* input, int input_len, char* output, int output_cap, int& output_len) {
    const int warning_threshold = static_cast<int>(runtime_json::extract_named_int(input, input_len, "warning_threshold", 580));
    const int sample_count = static_cast<int>(runtime_json::extract_named_int(input, input_len, "sample_count", 0));
    const int min_value = static_cast<int>(runtime_json::extract_named_int(input, input_len, "min_value", 0));
    const int max_value = static_cast<int>(runtime_json::extract_named_int(input, input_len, "max_value", 0));
    const int avg_value = static_cast<int>(runtime_json::extract_named_int(input, input_len, "avg_value", 0));
    const int checksum = static_cast<int>(runtime_json::extract_named_int(input, input_len, "checksum", 0));

    const int spread = max_value - min_value;
    const int score = avg_value + spread / 2 + (checksum % 97);

    runtime_json::JsonWriter json(output, output_cap);
    json.begin_object();
    json.field("sample_count", sample_count);
    json.field("avg_value", avg_value);
    json.field("spread", spread);
    json.field("score", score);
    json.field("risk_level", risk_label(score, warning_threshold));
    json.field("above_threshold", score >= warning_threshold);
    output_len = json.end_object();
    return json.ok() ? 0 : 2;
}
