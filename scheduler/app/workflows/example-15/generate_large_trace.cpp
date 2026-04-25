#include <cstdio>

#include "../common/runtime_json.hpp"
#include "../common/runtime_node.hpp"
#include "../common/runtime_random.hpp"

// Node: generate-audit-trace
//
// This node intentionally emits a large text payload. Its job is not domain
// realism; it exists to exercise the runtime path for large dependency outputs.
//
// Authoring note:
// downstream workflow code should not care whether a dependency was delivered
// inline or reassembled from chunks. The runtime handles that transport detail.

namespace {
uint64_t next_u64(uint64_t& state) {
    state ^= state << 13;
    state ^= state >> 7;
    state ^= state << 17;
    return state;
}
}  // namespace

WORKFLOW_JSON_NODE(8192, 262144)

int workflow_run_json(const char* input, int input_len, char* output, int output_cap, int& output_len) {
    int target = static_cast<int>(runtime_json::extract_named_int(input, input_len, "target_chars", 230000));
    if (target < 1024) {
        target = 1024;
    }
    if (target > output_cap - 128) {
        target = output_cap - 128;
    }

    uint64_t rng = runtime_random::seed_u64();
    int pos = std::snprintf(output, output_cap, "trace_id=%llu\n", static_cast<unsigned long long>(rng));
    int row = 0;
    // Fill the output buffer line by line until the requested size is reached.
    // The scheduler may later serve this output in chunks, but node code still
    // just produces a normal string result.
    while (pos > 0 && pos + 80 < target) {
        const unsigned long long a = static_cast<unsigned long long>(next_u64(rng) % 1000000ULL);
        const unsigned long long b = static_cast<unsigned long long>(next_u64(rng) % 1000000ULL);
        const int n = std::snprintf(output + pos, output_cap - pos, "event=%06d latency=%llu load=%llu\n", row, a, b);
        if (n <= 0 || n >= output_cap - pos) {
            break;
        }
        pos += n;
        ++row;
    }
    output_len = pos;
    return output_len > 0 ? 0 : 2;
}
