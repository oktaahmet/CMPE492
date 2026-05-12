#include "../common/workflow.hpp"

namespace {
const char* risk_label(int score, int warning_threshold) {
    if (score >= warning_threshold + 180) return "critical";
    if (score >= warning_threshold) return "watch";
    return "normal";
}
}  // namespace

WORKFLOW_NODE_WITH_CAPS(input, output, 8192, 1024) {
    const int warning_threshold = input.int_("warning_threshold", 580);

    const auto collect = input.node("collect-metrics");
    const int sample_count = collect.int_("sample_count", 0);
    const int min_value    = collect.int_("min_value", 0);
    const int max_value    = collect.int_("max_value", 0);
    const int avg_value    = collect.int_("avg_value", 0);
    const int checksum     = collect.int_("checksum", 0);

    const int spread = max_value - min_value;
    const int score = avg_value + spread / 2 + (checksum % 97);

    auto json = output.object();
    json.field("sample_count", sample_count);
    json.field("avg_value", avg_value);
    json.field("spread", spread);
    json.field("score", score);
    json.field("risk_level", risk_label(score, warning_threshold));
    json.field("above_threshold", score >= warning_threshold);
    json.done();
}
