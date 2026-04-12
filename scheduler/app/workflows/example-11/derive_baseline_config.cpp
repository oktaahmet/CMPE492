#include <cstdint>
#include <cstdio>

#ifdef __EMSCRIPTEN__
#include <emscripten/emscripten.h>
#else
#define EMSCRIPTEN_KEEPALIVE
#endif

#include "../common/runtime_json.hpp"

namespace {
constexpr int kInputCap = 64 * 1024;
constexpr int kOutputCap = 1024;

#ifdef __EMSCRIPTEN__
using runtime_ptr_t = int;
#else
using runtime_ptr_t = intptr_t;
#endif

unsigned char g_input[kInputCap];
char g_output[kOutputCap];
int g_output_len = 0;
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
    const int metadata_bytes = static_cast<int>(runtime_json::extract_named_int(in, input_len, "metadata_bytes", 0));
    const int metadata_word_count = static_cast<int>(runtime_json::extract_named_int(in, input_len, "metadata_word_count", 0));
    const int metadata_digit_count = static_cast<int>(runtime_json::extract_named_int(in, input_len, "metadata_digit_count", 0));
    const int metadata_queue_mentions = static_cast<int>(runtime_json::extract_named_int(in, input_len, "metadata_queue_mentions", 0));
    const int metadata_title_mentions = static_cast<int>(runtime_json::extract_named_int(in, input_len, "metadata_title_mentions", 0));
    if (metadata_bytes <= 0) {
        return 2;
    }

    const int customer_floor = static_cast<int>(runtime_json::extract_named_int(in, input_len, "customer_floor", 420));
    const int customer_span = static_cast<int>(runtime_json::extract_named_int(in, input_len, "customer_span", 220));
    const int interarrival_floor_ms = static_cast<int>(runtime_json::extract_named_int(in, input_len, "interarrival_floor_ms", 92));
    const int service_floor_ms = static_cast<int>(runtime_json::extract_named_int(in, input_len, "service_floor_ms", 68));
    const int stress_floor_pct = static_cast<int>(runtime_json::extract_named_int(in, input_len, "stress_floor_pct", 130));

    const int customers = customer_floor + (metadata_bytes % (customer_span <= 0 ? 1 : customer_span));
    const int mean_interarrival_ms = interarrival_floor_ms + (metadata_word_count % 45);
    const int mean_service_ms = service_floor_ms + ((metadata_queue_mentions * 7 + metadata_title_mentions * 3 + metadata_digit_count) % 40);
    const int stress_scale_pct = stress_floor_pct + (metadata_digit_count % 31);

    g_output_len = std::snprintf(
        g_output,
        kOutputCap,
        "{\"customers\":%d,\"mean_interarrival_ms\":%d,\"mean_service_ms\":%d,\"stress_scale_pct\":%d,\"metadata_bytes\":%d,\"metadata_queue_mentions\":%d}",
        customers,
        mean_interarrival_ms,
        mean_service_ms,
        stress_scale_pct,
        metadata_bytes,
        metadata_queue_mentions);
    return g_output_len < 0 || g_output_len >= kOutputCap ? 3 : 0;
}
}
