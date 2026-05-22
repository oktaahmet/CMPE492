#include "../common/workflow.hpp"

namespace {
int clamp_int(long long value, int min_value, int max_value) {
    if (value < min_value) return min_value;
    if (value > max_value) return max_value;
    return static_cast<int>(value);
}
}  // namespace

WORKFLOW_NODE(input, output) {
    const int seed = clamp_int(input.number("seed", 42), 1, 1000000);
    const int samples = clamp_int(input.number("samples", 18), 4, 64);

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

    auto json = output.object();
    json.field("sample_count", samples);
    json.field("min_value", min_value);
    json.field("max_value", max_value);
    json.field("avg_value", total / samples);
    json.field("checksum", checksum);
    json.done();
}
