#include <cstdio>

#include "../common/runtime_json.hpp"
#include "../common/runtime_node.hpp"

namespace {
constexpr int kMaxSamples = 512;

int average_of(const long long* values, int count) {
    if (!values || count <= 0) {
        return 0;
    }
    long long total = 0;
    for (int i = 0; i < count; ++i) {
        total += values[i];
    }
    return static_cast<int>(total / count);
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
}  // namespace

WORKFLOW_JSON_NODE(2 * 1024 * 1024, 1536)

int workflow_run_json(const char* in, int input_len, char* output, int output_cap, int& output_len) {
    long long stress_waits[kMaxSamples];
    long long stress_systems[kMaxSamples];
    long long stress_maxes[kMaxSamples];
    long long stress_served[kMaxSamples];
    long long stress_events[kMaxSamples];
    long long stress_replications[kMaxSamples];
    long long stress_utilizations[kMaxSamples];

    const int stress_wait_count = runtime_json::extract_named_ints(in, input_len, "avg_wait_ms", stress_waits, kMaxSamples);
    const int stress_system_count = runtime_json::extract_named_ints(in, input_len, "avg_system_ms", stress_systems, kMaxSamples);
    const int stress_max_count = runtime_json::extract_named_ints(in, input_len, "max_wait_ms", stress_maxes, kMaxSamples);
    const int stress_served_count = runtime_json::extract_named_ints(in, input_len, "served_customers", stress_served, kMaxSamples);
    const int stress_event_count = runtime_json::extract_named_ints(in, input_len, "total_customer_events", stress_events, kMaxSamples);
    const int stress_replication_count = runtime_json::extract_named_ints(in, input_len, "replications_per_worker", stress_replications, kMaxSamples);
    const int stress_utilization_count = runtime_json::extract_named_ints(in, input_len, "mean_utilization_pct", stress_utilizations, kMaxSamples);
    if (stress_wait_count <= 0 || stress_system_count != stress_wait_count || stress_max_count != stress_wait_count ||
        stress_served_count != stress_wait_count || stress_event_count != stress_wait_count ||
        stress_replication_count != stress_wait_count || stress_utilization_count != stress_wait_count) {
        return 2;
    }

    const int expected_stress_samples = static_cast<int>(runtime_json::extract_named_int(in, input_len, "expected_stress_samples", stress_wait_count));
    const int baseline_sample_count = static_cast<int>(runtime_json::extract_named_int(in, input_len, "baseline_sample_count", 0));
    const int expected_baseline_samples = static_cast<int>(runtime_json::extract_named_int(in, input_len, "expected_baseline_samples", baseline_sample_count));
    const int baseline_checks_ok = static_cast<int>(runtime_json::extract_named_int(in, input_len, "baseline_checks_ok", 0));
    const int baseline_mean_avg_wait_ms = static_cast<int>(runtime_json::extract_named_int(in, input_len, "baseline_mean_avg_wait_ms", 0));
    const int baseline_mean_avg_system_ms = static_cast<int>(runtime_json::extract_named_int(in, input_len, "baseline_mean_avg_system_ms", 0));
    const int baseline_mean_max_wait_ms = static_cast<int>(runtime_json::extract_named_int(in, input_len, "baseline_mean_max_wait_ms", 0));
    const int baseline_mean_total_events = static_cast<int>(runtime_json::extract_named_int(in, input_len, "baseline_mean_total_events", 0));
    const int baseline_mean_utilization_pct = static_cast<int>(runtime_json::extract_named_int(in, input_len, "baseline_mean_utilization_pct", 0));
    const int metadata_bytes = static_cast<int>(runtime_json::extract_named_int(in, input_len, "metadata_bytes", 0));
    const int metadata_queue_mentions = static_cast<int>(runtime_json::extract_named_int(in, input_len, "metadata_queue_mentions", 0));
    const int reference_word_count = static_cast<int>(runtime_json::extract_named_int(in, input_len, "reference_word_count", 0));
    const int reference_law_mentions = static_cast<int>(runtime_json::extract_named_int(in, input_len, "reference_law_mentions", 0));

    const int stress_mean_avg_wait_ms = average_of(stress_waits, stress_wait_count);
    const int stress_mean_avg_system_ms = average_of(stress_systems, stress_wait_count);
    const int stress_mean_max_wait_ms = average_of(stress_maxes, stress_wait_count);
    const int stress_mean_served_customers = average_of(stress_served, stress_wait_count);
    const int stress_mean_total_events = average_of(stress_events, stress_wait_count);
    const int stress_mean_replications = average_of(stress_replications, stress_wait_count);
    const int stress_mean_utilization_pct = average_of(stress_utilizations, stress_wait_count);
    const int avg_wait_increase_pct = percent_delta(baseline_mean_avg_wait_ms, stress_mean_avg_wait_ms);
    const int avg_system_increase_pct = percent_delta(baseline_mean_avg_system_ms, stress_mean_avg_system_ms);
    const int stress_checks_ok =
        (stress_wait_count == expected_stress_samples && stress_mean_avg_system_ms >= stress_mean_avg_wait_ms && stress_mean_total_events > baseline_mean_total_events) ? 1 : 0;
    const int all_checks_ok =
        (baseline_checks_ok == 1 && stress_checks_ok == 1 && baseline_sample_count == expected_baseline_samples) ? 1 : 0;

    output_len = std::snprintf(
        output,
        output_cap,
        "{\"metadata_bytes\":%d,\"metadata_queue_mentions\":%d,\"reference_word_count\":%d,\"reference_law_mentions\":%d,\"baseline_sample_count\":%d,\"expected_baseline_samples\":%d,\"stress_sample_count\":%d,\"expected_stress_samples\":%d,\"baseline_checks_ok\":%d,\"stress_checks_ok\":%d,\"all_checks_ok\":%d,\"baseline_mean_avg_wait_ms\":%d,\"stress_mean_avg_wait_ms\":%d,\"baseline_mean_avg_system_ms\":%d,\"stress_mean_avg_system_ms\":%d,\"baseline_mean_max_wait_ms\":%d,\"stress_mean_max_wait_ms\":%d,\"baseline_mean_utilization_pct\":%d,\"stress_mean_utilization_pct\":%d,\"stress_mean_served_customers\":%d,\"stress_mean_replications\":%d,\"baseline_mean_total_events\":%d,\"stress_mean_total_events\":%d,\"avg_wait_increase_pct\":%d,\"avg_system_increase_pct\":%d,\"verdict\":\"%s\"}",
        metadata_bytes,
        metadata_queue_mentions,
        reference_word_count,
        reference_law_mentions,
        baseline_sample_count,
        expected_baseline_samples,
        stress_wait_count,
        expected_stress_samples,
        baseline_checks_ok,
        stress_checks_ok,
        all_checks_ok,
        baseline_mean_avg_wait_ms,
        stress_mean_avg_wait_ms,
        baseline_mean_avg_system_ms,
        stress_mean_avg_system_ms,
        baseline_mean_max_wait_ms,
        stress_mean_max_wait_ms,
        baseline_mean_utilization_pct,
        stress_mean_utilization_pct,
        stress_mean_served_customers,
        stress_mean_replications,
        baseline_mean_total_events,
        stress_mean_total_events,
        avg_wait_increase_pct,
        avg_system_increase_pct,
        verdict_for(avg_wait_increase_pct));
    return output_len < 0 || output_len >= output_cap ? 3 : 0;
}
