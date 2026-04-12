#include <cctype>
#include <cstdint>
#include <cstdio>
#include <cstring>

#ifdef __EMSCRIPTEN__
#include <emscripten/emscripten.h>
#else
#define EMSCRIPTEN_KEEPALIVE
#endif

#include "../common/runtime_json.hpp"

namespace {
constexpr int kInputCap = 2 * 1024 * 1024;
constexpr int kOutputCap = 1536;
constexpr int kMaxSamples = 512;

#ifdef __EMSCRIPTEN__
using runtime_ptr_t = int;
#else
using runtime_ptr_t = intptr_t;
#endif

unsigned char g_input[kInputCap];
char g_output[kOutputCap];
int g_output_len = 0;

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

int extract_named_ints_local(const char* data, int len, const char* key, long long* out, int max_out) {
    if (!data || len <= 0 || !key || !out || max_out <= 0) {
        return 0;
    }
    const int key_len = static_cast<int>(std::strlen(key));
    int found = 0;
    for (int i = 0; i + key_len + 3 < len && found < max_out; ++i) {
        if (data[i] != '"') {
            continue;
        }
        bool match = true;
        for (int j = 0; j < key_len; ++j) {
            if (data[i + 1 + j] != key[j]) {
                match = false;
                break;
            }
        }
        if (!match || data[i + 1 + key_len] != '"') {
            continue;
        }
        int k = i + 1 + key_len + 1;
        while (k < len && data[k] != ':') {
            ++k;
        }
        if (k >= len) {
            continue;
        }
        ++k;
        while (k < len && std::isspace(static_cast<unsigned char>(data[k]))) {
            ++k;
        }
        int sign = 1;
        if (k < len && data[k] == '-') {
            sign = -1;
            ++k;
        }
        if (k >= len || !std::isdigit(static_cast<unsigned char>(data[k]))) {
            continue;
        }
        long long value = 0;
        while (k < len && std::isdigit(static_cast<unsigned char>(data[k]))) {
            value = value * 10 + static_cast<long long>(data[k] - '0');
            ++k;
        }
        out[found++] = value * sign;
    }
    return found;
}
}  // namespace

extern "C" {
EMSCRIPTEN_KEEPALIVE runtime_ptr_t get_input_ptr() { return static_cast<runtime_ptr_t>(reinterpret_cast<uintptr_t>(g_input)); }
EMSCRIPTEN_KEEPALIVE int get_input_capacity() { return kInputCap; }
EMSCRIPTEN_KEEPALIVE runtime_ptr_t get_output_ptr() { return static_cast<runtime_ptr_t>(reinterpret_cast<uintptr_t>(g_output)); }
EMSCRIPTEN_KEEPALIVE int get_output_len() { return g_output_len; }

EMSCRIPTEN_KEEPALIVE int run_json(int input_len) {
    if (input_len < 0 || input_len > kInputCap) {
        return 1;
    }

    const char* in = reinterpret_cast<const char*>(g_input);
    long long waits[kMaxSamples];
    long long systems[kMaxSamples];
    long long maxes[kMaxSamples];
    long long served[kMaxSamples];
    long long replications[kMaxSamples];
    long long total_events[kMaxSamples];
    long long utilizations[kMaxSamples];

    const int wait_count = extract_named_ints_local(in, input_len, "avg_wait_ms", waits, kMaxSamples);
    const int system_count = extract_named_ints_local(in, input_len, "avg_system_ms", systems, kMaxSamples);
    const int max_count = extract_named_ints_local(in, input_len, "max_wait_ms", maxes, kMaxSamples);
    const int served_count = extract_named_ints_local(in, input_len, "served_customers", served, kMaxSamples);
    const int replication_count = extract_named_ints_local(in, input_len, "replications_per_worker", replications, kMaxSamples);
    const int total_event_count = extract_named_ints_local(in, input_len, "total_customer_events", total_events, kMaxSamples);
    const int utilization_count = extract_named_ints_local(in, input_len, "mean_utilization_pct", utilizations, kMaxSamples);
    if (wait_count <= 0 || system_count != wait_count || max_count != wait_count || served_count != wait_count ||
        replication_count != wait_count || total_event_count != wait_count || utilization_count != wait_count) {
        return 2;
    }

    const int expected_baseline_samples = static_cast<int>(runtime_json::extract_named_int(in, input_len, "expected_baseline_samples", wait_count));
    const int customers = static_cast<int>(runtime_json::extract_named_int(in, input_len, "customers", average_of(served, wait_count)));
    const int mean_interarrival_ms = static_cast<int>(runtime_json::extract_named_int(in, input_len, "mean_interarrival_ms", 120));
    const int mean_service_ms = static_cast<int>(runtime_json::extract_named_int(in, input_len, "mean_service_ms", 95));
    const int stress_scale_pct = static_cast<int>(runtime_json::extract_named_int(in, input_len, "stress_scale_pct", 135));
    const int stress_customer_bump = static_cast<int>(runtime_json::extract_named_int(in, input_len, "stress_customer_bump", 90));
    const int stress_interarrival_floor_ms = static_cast<int>(runtime_json::extract_named_int(in, input_len, "stress_interarrival_floor_ms", 28));
    const int stress_service_bump_ms = static_cast<int>(runtime_json::extract_named_int(in, input_len, "stress_service_bump_ms", 9));

    const int baseline_mean_avg_wait_ms = average_of(waits, wait_count);
    const int baseline_mean_avg_system_ms = average_of(systems, wait_count);
    const int baseline_mean_max_wait_ms = average_of(maxes, wait_count);
    const int baseline_mean_replications = average_of(replications, wait_count);
    const int baseline_mean_total_events = average_of(total_events, wait_count);
    const int baseline_mean_utilization_pct = average_of(utilizations, wait_count);
    const int baseline_checks_ok =
        (wait_count == expected_baseline_samples && baseline_mean_avg_system_ms >= baseline_mean_avg_wait_ms && baseline_mean_total_events > 0) ? 1 : 0;

    const int planned_customers = customers + stress_customer_bump + wait_count * 12;
    const int scaled_interarrival = mean_interarrival_ms * 100 / (stress_scale_pct <= 0 ? 100 : stress_scale_pct);
    const int planned_interarrival_ms = scaled_interarrival > stress_interarrival_floor_ms ? scaled_interarrival : stress_interarrival_floor_ms;
    const int planned_service_ms = mean_service_ms + stress_service_bump_ms + (baseline_mean_avg_wait_ms / 22);

    g_output_len = std::snprintf(
        g_output,
        kOutputCap,
        "{\"baseline_sample_count\":%d,\"expected_baseline_samples\":%d,\"baseline_checks_ok\":%d,\"baseline_mean_avg_wait_ms\":%d,\"baseline_mean_avg_system_ms\":%d,\"baseline_mean_max_wait_ms\":%d,\"baseline_mean_replications\":%d,\"baseline_mean_total_events\":%d,\"baseline_mean_utilization_pct\":%d,\"customers\":%d,\"mean_interarrival_ms\":%d,\"mean_service_ms\":%d,\"planned_stress_scale_pct\":%d}",
        wait_count,
        expected_baseline_samples,
        baseline_checks_ok,
        baseline_mean_avg_wait_ms,
        baseline_mean_avg_system_ms,
        baseline_mean_max_wait_ms,
        baseline_mean_replications,
        baseline_mean_total_events,
        baseline_mean_utilization_pct,
        planned_customers,
        planned_interarrival_ms,
        planned_service_ms,
        stress_scale_pct);
    return g_output_len < 0 || g_output_len >= kOutputCap ? 3 : 0;
}
}
