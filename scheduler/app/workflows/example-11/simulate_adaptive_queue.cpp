#include <cmath>
#include <cstdint>
#include <cstdio>
#include <cstring>
#include <emscripten/emscripten.h>

#include "../common/runtime_json.hpp"
#include "../common/runtime_random.hpp"

namespace {
constexpr int kInputCap = 16 * 1024;
constexpr int kOutputCap = 1280;
constexpr int kScenarioCap = 32;

unsigned char g_input[kInputCap];
char g_output[kOutputCap];
int g_output_len = 0;

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
    const double u = uniform_0_1(state);
    return -std::log(1.0 - u) * static_cast<double>(mean_ms);
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
    const double normalized = acc / static_cast<double>(chaos_steps);
    const double candidate = 1.0 + normalized * 0.18;
    if (candidate < 0.72) return 0.72;
    if (candidate > 1.28) return 1.28;
    return candidate;
}

void load_scenario_name(const char* in, int input_len, char* out, int out_cap) {
    const char* ptr = nullptr;
    const int len = runtime_json::extract_named_string(in, input_len, "scenario_name", &ptr);
    if (!out || out_cap <= 0) {
        return;
    }
    if (!ptr || len <= 0) {
        const char* fallback = "baseline";
        const int n = static_cast<int>(std::strlen(fallback));
        const int copy = n < out_cap - 1 ? n : out_cap - 1;
        std::memcpy(out, fallback, static_cast<size_t>(copy));
        out[copy] = '\0';
        return;
    }
    const int copy = len < out_cap - 1 ? len : out_cap - 1;
    std::memcpy(out, ptr, static_cast<size_t>(copy));
    out[copy] = '\0';
}
}  // namespace

extern "C" {
EMSCRIPTEN_KEEPALIVE int get_input_ptr() { return static_cast<int>(reinterpret_cast<uintptr_t>(g_input)); }
EMSCRIPTEN_KEEPALIVE int get_input_capacity() { return kInputCap; }
EMSCRIPTEN_KEEPALIVE int get_output_ptr() { return static_cast<int>(reinterpret_cast<uintptr_t>(g_output)); }
EMSCRIPTEN_KEEPALIVE int get_output_len() { return g_output_len; }

EMSCRIPTEN_KEEPALIVE int run_json(int input_len) {
    if (input_len < 0 || input_len > kInputCap) {
        return 1;
    }

    const char* in = reinterpret_cast<const char*>(g_input);
    int customers = static_cast<int>(runtime_json::extract_named_int(
        in,
        input_len,
        "customers",
        runtime_json::extract_named_int(in, input_len, "fallback_customers", 500)));
    int mean_interarrival_ms = static_cast<int>(runtime_json::extract_named_int(
        in,
        input_len,
        "mean_interarrival_ms",
        runtime_json::extract_named_int(in, input_len, "fallback_mean_interarrival_ms", 120)));
    int mean_service_ms = static_cast<int>(runtime_json::extract_named_int(
        in,
        input_len,
        "mean_service_ms",
        runtime_json::extract_named_int(in, input_len, "fallback_mean_service_ms", 95)));
    const int replications_per_worker = static_cast<int>(runtime_json::extract_named_int(in, input_len, "replications_per_worker", 1));
    const int chaos_steps = static_cast<int>(runtime_json::extract_named_int(in, input_len, "chaos_steps", 0));

    const int reference_word_count = static_cast<int>(runtime_json::extract_named_int(in, input_len, "reference_word_count", 0));
    const int reference_law_mentions = static_cast<int>(runtime_json::extract_named_int(in, input_len, "reference_law_mentions", 0));
    const int reference_wait_mentions = static_cast<int>(runtime_json::extract_named_int(in, input_len, "reference_wait_mentions", 0));
    if (reference_word_count > 0 || reference_law_mentions > 0 || reference_wait_mentions > 0) {
        customers += reference_word_count % 35;
        mean_service_ms += reference_wait_mentions % 9;
        const int delta = reference_law_mentions % 11;
        mean_interarrival_ms = (mean_interarrival_ms - delta) > 25 ? (mean_interarrival_ms - delta) : 25;
    }

    if (customers <= 0 || mean_interarrival_ms <= 0 || mean_service_ms <= 0 || replications_per_worker <= 0) {
        return 2;
    }

    char scenario[kScenarioCap];
    load_scenario_name(in, input_len, scenario, kScenarioCap);

    uint64_t rng = runtime_random::seed_u64();
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
            const double chaos_multiplier = bounded_chaos_multiplier(&rng, chaos_steps);
            const double service_ms = sample_exponential_ms(mean_service_ms, &rng) * chaos_multiplier;
            const double service_start = arrival_time > server_free_at ? arrival_time : server_free_at;
            const double wait_ms = service_start - arrival_time;
            const double depart_at = service_start + service_ms;
            const double system_ms = depart_at - arrival_time;

            total_wait += wait_ms;
            total_system += system_ms;
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
    const int avg_wait_ms = static_cast<int>(total_wait / total_customers + 0.5);
    const int avg_system_ms = static_cast<int>(total_system / total_customers + 0.5);
    const int max_wait_ms = static_cast<int>(max_wait + 0.5);
    const int mean_utilization_pct = static_cast<int>(total_utilization / replications_per_worker + 0.5);

    g_output_len = std::snprintf(
        g_output,
        kOutputCap,
        "{\"scenario\":\"%s\",\"avg_wait_ms\":%d,\"avg_system_ms\":%d,\"max_wait_ms\":%d,\"served_customers\":%d,\"used_interarrival_ms\":%d,\"used_service_ms\":%d,\"replications_per_worker\":%d,\"chaos_steps\":%d,\"total_customer_events\":%d,\"mean_utilization_pct\":%d}",
        scenario,
        avg_wait_ms,
        avg_system_ms,
        max_wait_ms,
        customers,
        mean_interarrival_ms,
        mean_service_ms,
        replications_per_worker,
        chaos_steps,
        total_customers,
        mean_utilization_pct);
    return g_output_len < 0 || g_output_len >= kOutputCap ? 3 : 0;
}
}
