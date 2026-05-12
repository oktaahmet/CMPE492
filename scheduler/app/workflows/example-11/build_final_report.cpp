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

int percent_delta(int base, int current) {
    if (base <= 0) {
        return 0;
    }
    return static_cast<int>(((current - base) * 100LL) / base);
}

const char* verdict_for(int wait_delta_pct) {
    if (wait_delta_pct >= 80) return "stress_amplified";
    if (wait_delta_pct >= 35) return "stress_visible";
    return "stress_absorbed";
}

bool same_size(const std::vector<long long>& values, int expected) {
    return static_cast<int>(values.size()) == expected;
}
}  // namespace

WORKFLOW_NODE_WITH_CAPS(input, output, 2 * 1024 * 1024, 1536) {
    const auto stress   = input.node("run-stress-simulations");
    const auto baseline = input.node("summarize-baseline");
    const auto metadata = input.node("fetch-queueing-metadata");
    const auto reference = input.optional_node("fetch-reference-load-signal");

    const auto stress_waits        = stress.numbers("avg_wait_ms");
    const auto stress_systems      = stress.numbers("avg_system_ms");
    const auto stress_maxes        = stress.numbers("max_wait_ms");
    const auto stress_served       = stress.numbers("served_customers");
    const auto stress_events       = stress.numbers("total_customer_events");
    const auto stress_replications = stress.numbers("replications_per_worker");
    const auto stress_utilizations = stress.numbers("mean_utilization_pct");

    const int stress_sample_count = static_cast<int>(stress_waits.size());
    if (stress_sample_count <= 0 || !same_size(stress_systems, stress_sample_count) ||
        !same_size(stress_maxes, stress_sample_count) || !same_size(stress_served, stress_sample_count) ||
        !same_size(stress_events, stress_sample_count) || !same_size(stress_replications, stress_sample_count) ||
        !same_size(stress_utilizations, stress_sample_count)) {
        output.fail(30);
        return;
    }

    const int expected_stress_samples = input.int_("expected_stress_samples", stress_sample_count);
    const int baseline_sample_count = baseline.int_("baseline_sample_count", 0);
    const int expected_baseline_samples = baseline.int_("expected_baseline_samples", baseline_sample_count);
    const int baseline_checks_ok = baseline.int_("baseline_checks_ok", 0);
    const int baseline_mean_avg_wait_ms = baseline.int_("baseline_mean_avg_wait_ms", 0);
    const int baseline_mean_avg_system_ms = baseline.int_("baseline_mean_avg_system_ms", 0);
    const int baseline_mean_max_wait_ms = baseline.int_("baseline_mean_max_wait_ms", 0);
    const int baseline_mean_total_events = baseline.int_("baseline_mean_total_events", 0);
    const int baseline_mean_utilization_pct = baseline.int_("baseline_mean_utilization_pct", 0);
    const int metadata_bytes = metadata.int_("metadata_bytes", 0);
    const int metadata_queue_mentions = metadata.int_("metadata_queue_mentions", 0);
    const int reference_word_count = reference.int_("reference_word_count", 0);
    const int reference_law_mentions = reference.int_("reference_law_mentions", 0);

    const int stress_mean_avg_wait_ms = average_of(stress_waits);
    const int stress_mean_avg_system_ms = average_of(stress_systems);
    const int stress_mean_max_wait_ms = average_of(stress_maxes);
    const int stress_mean_served_customers = average_of(stress_served);
    const int stress_mean_total_events = average_of(stress_events);
    const int stress_mean_replications = average_of(stress_replications);
    const int stress_mean_utilization_pct = average_of(stress_utilizations);
    const int avg_wait_increase_pct = percent_delta(baseline_mean_avg_wait_ms, stress_mean_avg_wait_ms);
    const int avg_system_increase_pct = percent_delta(baseline_mean_avg_system_ms, stress_mean_avg_system_ms);
    const bool stress_checks_ok =
        stress_sample_count == expected_stress_samples &&
        stress_mean_avg_system_ms >= stress_mean_avg_wait_ms &&
        stress_mean_total_events > baseline_mean_total_events;
    const bool all_checks_ok =
        baseline_checks_ok == 1 && stress_checks_ok && baseline_sample_count == expected_baseline_samples;

    auto json = output.object();
    json.field("metadata_bytes", metadata_bytes);
    json.field("metadata_queue_mentions", metadata_queue_mentions);
    json.field("reference_word_count", reference_word_count);
    json.field("reference_law_mentions", reference_law_mentions);
    json.field("baseline_sample_count", baseline_sample_count);
    json.field("expected_baseline_samples", expected_baseline_samples);
    json.field("stress_sample_count", stress_sample_count);
    json.field("expected_stress_samples", expected_stress_samples);
    json.field("baseline_checks_ok", baseline_checks_ok);
    json.field("stress_checks_ok", stress_checks_ok ? 1 : 0);
    json.field("all_checks_ok", all_checks_ok ? 1 : 0);
    json.field("baseline_mean_avg_wait_ms", baseline_mean_avg_wait_ms);
    json.field("stress_mean_avg_wait_ms", stress_mean_avg_wait_ms);
    json.field("baseline_mean_avg_system_ms", baseline_mean_avg_system_ms);
    json.field("stress_mean_avg_system_ms", stress_mean_avg_system_ms);
    json.field("baseline_mean_max_wait_ms", baseline_mean_max_wait_ms);
    json.field("stress_mean_max_wait_ms", stress_mean_max_wait_ms);
    json.field("baseline_mean_utilization_pct", baseline_mean_utilization_pct);
    json.field("stress_mean_utilization_pct", stress_mean_utilization_pct);
    json.field("stress_mean_served_customers", stress_mean_served_customers);
    json.field("stress_mean_replications", stress_mean_replications);
    json.field("baseline_mean_total_events", baseline_mean_total_events);
    json.field("stress_mean_total_events", stress_mean_total_events);
    json.field("avg_wait_increase_pct", avg_wait_increase_pct);
    json.field("avg_system_increase_pct", avg_system_increase_pct);
    json.field("verdict", verdict_for(avg_wait_increase_pct));
    json.done();
}
