#include <cstdio>
#include <cstdint>
#include <string>

#include "../common/workflow.hpp"

namespace {
uint64_t next_u64(uint64_t& state) {
    state ^= state << 13;
    state ^= state >> 7;
    state ^= state << 17;
    return state;
}
}  // namespace

WORKFLOW_NODE_WITH_CAPS(input, output, 8192, 262144) {
    int target = input.int_("target_chars", 230000);
    if (target < 1024) {
        target = 1024;
    }
    if (target > 240000) {
        target = 240000;
    }

    uint64_t rng = workflow::random_seed();
    std::string trace;
    trace.reserve(static_cast<size_t>(target));

    char line[128];
    int written = std::snprintf(line, sizeof(line), "trace_id=%llu\n", static_cast<unsigned long long>(rng));
    if (written > 0) {
        trace.append(line, static_cast<size_t>(written));
    }

    int row = 0;
    while (static_cast<int>(trace.size()) + 80 < target) {
        const unsigned long long latency = static_cast<unsigned long long>(next_u64(rng) % 1000000ULL);
        const unsigned long long load = static_cast<unsigned long long>(next_u64(rng) % 1000000ULL);
        written = std::snprintf(
            line,
            sizeof(line),
            "event=%06d latency=%llu load=%llu\n",
            row,
            latency,
            load);
        if (written <= 0 || written >= static_cast<int>(sizeof(line))) {
            break;
        }
        trace.append(line, static_cast<size_t>(written));
        ++row;
    }

    output.text(trace);
}
