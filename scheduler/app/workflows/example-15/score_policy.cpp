#include "../common/workflow.hpp"

WORKFLOW_NODE_WITH_CAPS(input, output, 65536, 64) {
    const int target_wait_ms = input.int_("target_wait_ms", 850);
    const auto aggregate = input.node("aggregate-simulations");
    const int average_wait_ms = aggregate.int_("average_wait_ms");
    const int trace_sample_count = aggregate.int_("trace_sample_count");
    const int simulation_sample_count = aggregate.int_("simulation_sample_count");

    int score = 1000 - (average_wait_ms - target_wait_ms);
    score += trace_sample_count * 7 + simulation_sample_count * 3;
    if (score < 0) {
        score = 0;
    }

    output.number(score);
}
