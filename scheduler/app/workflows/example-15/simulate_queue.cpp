#include <cstdio>
#include <cstring>

#include "../common/runtime_json.hpp"
#include "../common/runtime_node.hpp"
#include "../common/runtime_random.hpp"

// Node: simulate-baseline / simulate-stress
//
// This browser-worker node runs a small stochastic queue simulation. The same
// C++ program is reused by two workflow nodes, with args deciding whether the
// run represents the baseline or stress scenario.
//
// Authoring note:
// stochastic work is a good fit for collect_all. Each worker can return one
// sample, and a later reducer can aggregate the full sample set into a stable
// downstream result.

namespace {
uint64_t next_u64(uint64_t& state) {
    state ^= state << 13;
    state ^= state >> 7;
    state ^= state << 17;
    return state;
}

const char* scenario_name(const char* input, int input_len) {
    const char* value = nullptr;
    const int len = runtime_json::extract_named_string(input, input_len, "scenario", &value);
    if (len == 6 && std::strncmp(value, "stress", 6) == 0) {
        return "stress";
    }
    return "baseline";
}
}  // namespace

WORKFLOW_JSON_NODE(65536, 1280)

int workflow_run_json(const char* input, int input_len, char* output, int output_cap, int& output_len) {
    // The workflow reuses one program for two nodes. Keeping the branching in
    // args is often cleaner than duplicating nearly identical C++ files.
    const char* scenario = scenario_name(input, input_len);
    const int customers = static_cast<int>(runtime_json::extract_named_int(input, input_len, "customers", 500));
    const int service_ms = static_cast<int>(runtime_json::extract_named_int(input, input_len, "service_ms", 80));
    const int arrival_ms = static_cast<int>(runtime_json::extract_named_int(input, input_len, "arrival_ms", 90));
    const int replications = static_cast<int>(runtime_json::extract_named_int(input, input_len, "replications", 16));
    const int source_words = static_cast<int>(runtime_json::extract_named_int(input, input_len, "source_words", 0));
    const int profile_checksum = static_cast<int>(runtime_json::extract_named_int(input, input_len, "profile_checksum", 0));
    const int average_service_from_profile =
        static_cast<int>(runtime_json::extract_named_int(input, input_len, "average_service_ms", service_ms));

    uint64_t rng =
        runtime_random::seed_u64() ^ static_cast<uint64_t>(customers * 131 + average_service_from_profile * 17 + profile_checksum);
    long long total_wait = 0;
    long long total_system = 0;
    int max_wait = 0;
    int served = 0;
    // Each replication uses the same high-level configuration but a different
    // random stream. This is the important difference between "replications
    // inside one worker result" and "collect_all across multiple workers".
    for (int r = 0; r < replications; ++r) {
        int backlog = static_cast<int>(next_u64(rng) % 23);
        for (int c = 0; c < customers; ++c) {
            const int jitter = static_cast<int>(next_u64(rng) % 41);
            backlog += service_ms + jitter - arrival_ms;
            if (backlog < 0) {
                backlog = 0;
            }
            const int wait = backlog + static_cast<int>(next_u64(rng) % 9);
            const int system = wait + service_ms;
            total_wait += wait;
            total_system += system;
            if (wait > max_wait) {
                max_wait = wait;
            }
            ++served;
        }
    }

    const int avg_wait_ms = served > 0 ? static_cast<int>(total_wait / served) : 0;
    const int avg_system_ms = served > 0 ? static_cast<int>(total_system / served) : 0;
    const int total_customer_events = customers * replications;
    const int utilization_pct = arrival_ms > 0 ? (service_ms * 100 / arrival_ms) : 0;

    // Return one explicit sample object. collect_all will wrap several of these
    // per-worker outputs into a samples array for the server reducer.
    output_len = std::snprintf(
        output,
        output_cap,
        "{\"scenario\":\"%s\",\"avg_wait_ms\":%d,\"avg_system_ms\":%d,\"max_wait_ms\":%d,\"served_customers\":%d,\"total_customer_events\":%d,\"replications\":%d,\"utilization_pct\":%d,\"source_words\":%d,\"profile_checksum\":%d}",
        scenario,
        avg_wait_ms,
        avg_system_ms,
        max_wait,
        served,
        total_customer_events,
        replications,
        utilization_pct,
        source_words,
        profile_checksum);
    return output_len < 0 || output_len >= output_cap ? 2 : 0;
}
