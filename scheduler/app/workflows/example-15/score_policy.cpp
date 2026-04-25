#include <cstdio>

#include "../common/runtime_json.hpp"
#include "../common/runtime_node.hpp"

// Node: score-policy
//
// This node turns the aggregate summary into a single numeric score. It exists
// mainly to demonstrate a primitive number output instead of an object.
//
// Authoring note:
// not every node needs to return structured JSON. If the result is genuinely a
// single scalar, returning a number can make the workflow contract simpler.

WORKFLOW_JSON_NODE(65536, 64)

int workflow_run_json(const char* input, int input_len, char* output, int output_cap, int& output_len) {
    const int target_wait_ms = static_cast<int>(runtime_json::extract_named_int(input, input_len, "target_wait_ms", 850));
    const int average_wait_ms = static_cast<int>(runtime_json::extract_named_int(input, input_len, "average_wait_ms", 0));
    const int trace_sample_count = static_cast<int>(runtime_json::extract_named_int(input, input_len, "trace_sample_count", 0));
    const int simulation_sample_count = static_cast<int>(runtime_json::extract_named_int(input, input_len, "simulation_sample_count", 0));

    // Keep the scoring rule intentionally simple. The important part for this
    // example is the output shape and dependency consumption, not the scoring
    // formula itself.
    int score = 1000 - (average_wait_ms - target_wait_ms);
    score += trace_sample_count * 7 + simulation_sample_count * 3;
    if (score < 0) {
        score = 0;
    }
    output_len = std::snprintf(output, output_cap, "%d", score);
    return output_len < 0 || output_len >= output_cap ? 2 : 0;
}
