#include "../common/workflow.hpp"

namespace {
int average_of(const std::vector<long long>& values) {
    if (values.empty()) {
        return 0;
    }
    long long total = 0;
    for (const long long v : values) {
        total += v;
    }
    return static_cast<int>(total / static_cast<long long>(values.size()));
}

bool same_size(const std::vector<long long>& values, int expected) {
    return static_cast<int>(values.size()) == expected;
}
}  // namespace

WORKFLOW_NODE_WITH_CAPS(input, output, 2 * 1024 * 1024, 1536) {
    const auto baseline = input.node("run-baseline-simulations");
    const auto config = input.node("derive-baseline-config");

    const auto waits        = baseline.numbers("avg_wait_ms");
    const auto systems      = baseline.numbers("avg_system_ms");
    const auto maxes        = baseline.numbers("max_wait_ms");
    const auto served       = baseline.numbers("served_customers");
    const auto replications = baseline.numbers("replications_per_worker");
    const auto total_events = baseline.numbers("total_customer_events");
    const auto utilizations = baseline.numbers("mean_utilization_pct");

    const int sample_count = static_cast<int>(waits.size());
    if (sample_count <= 0 || !same_size(systems, sample_count) || !same_size(maxes, sample_count) ||
        !same_size(served, sample_count) || !same_size(replications, sample_count) ||
        !same_size(total_events, sample_count) || !same_size(utilizations, sample_count)) {
        output.fail(30);
        return;
    }

    const int expected_baseline_samples = input.int_("expected_baseline_samples", sample_count);
    const int customers = config.int_("customers", average_of(served));
    const int mean_interarrival_ms = config.int_("mean_interarrival_ms", 120);
    const int mean_service_ms = config.int_("mean_service_ms", 95);
    const int stress_scale_pct = config.int_("stress_scale_pct", 135);
    const int stress_customer_bump = input.int_("stress_customer_bump", 90);
    const int stress_interarrival_floor_ms = input.int_("stress_interarrival_floor_ms", 28);
    const int stress_service_bump_ms = input.int_("stress_service_bump_ms", 9);

    const int baseline_mean_avg_wait_ms = average_of(waits);
    const int baseline_mean_avg_system_ms = average_of(systems);
    const int baseline_mean_max_wait_ms = average_of(maxes);
    const int baseline_mean_replications = average_of(replications);
    const int baseline_mean_total_events = average_of(total_events);
    const int baseline_mean_utilization_pct = average_of(utilizations);
    const bool baseline_checks_ok =
        sample_count == expected_baseline_samples &&
        baseline_mean_avg_system_ms >= baseline_mean_avg_wait_ms &&
        baseline_mean_total_events > 0;

    const int planned_customers = customers + stress_customer_bump + sample_count * 12;
    const int scaled_interarrival = mean_interarrival_ms * 100 / (stress_scale_pct <= 0 ? 100 : stress_scale_pct);
    const int planned_interarrival_ms = scaled_interarrival > stress_interarrival_floor_ms
        ? scaled_interarrival
        : stress_interarrival_floor_ms;
    const int planned_service_ms = mean_service_ms + stress_service_bump_ms + (baseline_mean_avg_wait_ms / 22);

    auto json = output.object();
    json.field("baseline_sample_count", sample_count);
    json.field("expected_baseline_samples", expected_baseline_samples);
    json.field("baseline_checks_ok", baseline_checks_ok ? 1 : 0);
    json.field("baseline_mean_avg_wait_ms", baseline_mean_avg_wait_ms);
    json.field("baseline_mean_avg_system_ms", baseline_mean_avg_system_ms);
    json.field("baseline_mean_max_wait_ms", baseline_mean_max_wait_ms);
    json.field("baseline_mean_replications", baseline_mean_replications);
    json.field("baseline_mean_total_events", baseline_mean_total_events);
    json.field("baseline_mean_utilization_pct", baseline_mean_utilization_pct);
    json.field("customers", planned_customers);
    json.field("mean_interarrival_ms", planned_interarrival_ms);
    json.field("mean_service_ms", planned_service_ms);
    json.field("planned_stress_scale_pct", stress_scale_pct);
    json.done();
}
