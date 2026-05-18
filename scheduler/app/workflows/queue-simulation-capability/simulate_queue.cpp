#include <cstdint>
#include <cstring>

#include "../common/workflow.hpp"

namespace {
uint64_t next_u64(uint64_t& state) {
    state ^= state << 13;
    state ^= state >> 7;
    state ^= state << 17;
    return state;
}

const char* scenario_name(std::string_view value) {
    return value == "stress" ? "stress" : "baseline";
}
}  // namespace

WORKFLOW_NODE_WITH_CAPS(input, output, 65536, 1280) {
    const char* scenario = scenario_name(input.string("scenario"));
    const int customers = input.int_("customers", 500);
    const int service_ms = input.int_("service_ms", 80);
    const int arrival_ms = input.int_("arrival_ms", 90);
    const int replications = input.int_("replications", 16);

    const int source_words = input.node("fetch-public-signal").int_("source_words");
    const int profile_checksum = input.node("read-load-profile").int_("profile_checksum");
    const int average_service_from_profile = input.node("read-load-profile").int_("average_service_ms", service_ms);

    uint64_t rng =
        workflow::random_seed() ^ static_cast<uint64_t>(customers * 131 + average_service_from_profile * 17 + profile_checksum);
    long long total_wait = 0;
    long long total_system = 0;
    int max_wait = 0;
    int served = 0;

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

    auto json = output.object();
    json.field("scenario", scenario);
    json.field("avg_wait_ms", avg_wait_ms);
    json.field("avg_system_ms", avg_system_ms);
    json.field("max_wait_ms", max_wait);
    json.field("served_customers", served);
    json.field("total_customer_events", total_customer_events);
    json.field("replications", replications);
    json.field("utilization_pct", utilization_pct);
    json.field("source_words", source_words);
    json.field("profile_checksum", profile_checksum);
    json.done();
}
