#include <cmath>
#include <cstring>

#include "../common/workflow.hpp"

namespace {
uint64_t next_u64(uint64_t* state) {
    uint64_t x = *state;
    x ^= x << 13;
    x ^= x >> 7;
    x ^= x << 17;
    *state = x;
    return x;
}

double uniform_0_1(uint64_t* state) {
    const uint64_t raw = next_u64(state);
    const double unit = static_cast<double>(raw >> 11) * (1.0 / 9007199254740992.0);
    if (unit <= 0.0) return 1e-12;
    if (unit >= 1.0) return 1.0 - 1e-12;
    return unit;
}

double sample_exponential_ms(int mean_ms, uint64_t* state) {
    return -std::log(1.0 - uniform_0_1(state)) * static_cast<double>(mean_ms);
}

double bounded_chaos_multiplier(uint64_t* state, int chaos_steps) {
    if (chaos_steps <= 0) {
        return 1.0;
    }
    double acc = 0.0;
    for (int i = 0; i < chaos_steps; ++i) {
        const double u = uniform_0_1(state);
        acc += std::sin(u * 6.283185307179586) * 0.5;
        acc += std::cos(u * 3.141592653589793) * 0.25;
    }
    const double candidate = 1.0 + (acc / static_cast<double>(chaos_steps)) * 0.18;
    if (candidate < 0.72) return 0.72;
    if (candidate > 1.28) return 1.28;
    return candidate;
}
}  // namespace

WORKFLOW_NODE_WITH_CAPS(input, output, 16 * 1024, 1280) {
    int customers = input.node("derive-baseline-config").int_("customers", input.int_("fallback_customers", 500));
    int mean_interarrival_ms = input.node("derive-baseline-config").int_(
        "mean_interarrival_ms",
        input.int_("fallback_mean_interarrival_ms", 120));
    int mean_service_ms = input.node("derive-baseline-config").int_(
        "mean_service_ms",
        input.int_("fallback_mean_service_ms", 95));
    const int replications_per_worker = input.int_("replications_per_worker", 1);
    const int chaos_steps = input.int_("chaos_steps", 0);

    const workflow::Object reference = input.optional_node("fetch-reference-load-signal");
    const int reference_word_count = reference.int_("reference_word_count", 0);
    const int reference_law_mentions = reference.int_("reference_law_mentions", 0);
    const int reference_wait_mentions = reference.int_("reference_wait_mentions", 0);
    if (reference_word_count > 0 || reference_law_mentions > 0 || reference_wait_mentions > 0) {
        customers += reference_word_count % 35;
        mean_service_ms += reference_wait_mentions % 9;
        const int delta = reference_law_mentions % 11;
        mean_interarrival_ms = (mean_interarrival_ms - delta) > 25 ? (mean_interarrival_ms - delta) : 25;
    }

    if (customers <= 0 || mean_interarrival_ms <= 0 || mean_service_ms <= 0 || replications_per_worker <= 0) {
        output.fail(2);
        return;
    }

    const std::string_view scenario = input.string("scenario_name");
    uint64_t rng = workflow::random_seed();
    double total_wait = 0.0;
    double total_system = 0.0;
    double max_wait = 0.0;
    double total_utilization = 0.0;

    for (int replication = 0; replication < replications_per_worker; ++replication) {
        double arrival_time = 0.0;
        double server_free_at = 0.0;
        double busy_service_time = 0.0;
        for (int i = 0; i < customers; ++i) {
            arrival_time += sample_exponential_ms(mean_interarrival_ms, &rng);
            const double service_ms = sample_exponential_ms(mean_service_ms, &rng) *
                bounded_chaos_multiplier(&rng, chaos_steps);
            const double service_start = arrival_time > server_free_at ? arrival_time : server_free_at;
            const double wait_ms = service_start - arrival_time;
            const double depart_at = service_start + service_ms;

            total_wait += wait_ms;
            total_system += depart_at - arrival_time;
            if (wait_ms > max_wait) {
                max_wait = wait_ms;
            }
            server_free_at = depart_at;
            busy_service_time += service_ms;
        }
        if (server_free_at > 1e-9) {
            total_utilization += (busy_service_time / server_free_at) * 100.0;
        }
    }

    const int total_customers = customers * replications_per_worker;
    auto json = output.object();
    json.field("scenario", scenario.empty() ? "baseline" : std::string(scenario).c_str());
    json.field("avg_wait_ms", static_cast<int>(total_wait / total_customers + 0.5));
    json.field("avg_system_ms", static_cast<int>(total_system / total_customers + 0.5));
    json.field("max_wait_ms", static_cast<int>(max_wait + 0.5));
    json.field("served_customers", customers);
    json.field("used_interarrival_ms", mean_interarrival_ms);
    json.field("used_service_ms", mean_service_ms);
    json.field("replications_per_worker", replications_per_worker);
    json.field("chaos_steps", chaos_steps);
    json.field("total_customer_events", total_customers);
    json.field("mean_utilization_pct", static_cast<int>(total_utilization / replications_per_worker + 0.5));
    json.done();
}
