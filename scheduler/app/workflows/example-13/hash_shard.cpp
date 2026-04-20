#include <climits>
#include <cstdint>
#include <cstdio>

#include "../common/runtime_artifacts.hpp"
#include "../common/runtime_json.hpp"
#include "../common/runtime_node.hpp"

namespace {
constexpr int kURLCap = 1024;
constexpr int kNumberCount = 1000000;
constexpr int kNumbersBytes = kNumberCount * 4;
constexpr int kHashRounds = 2048;
constexpr uint64_t kMatchMask = 1023ULL;

unsigned char g_numbers[kNumbersBytes];
char g_matches[95 * 1024];

uint32_t read_u32_le(const unsigned char* data, int offset) {
    return static_cast<uint32_t>(data[offset]) |
           (static_cast<uint32_t>(data[offset + 1]) << 8) |
           (static_cast<uint32_t>(data[offset + 2]) << 16) |
           (static_cast<uint32_t>(data[offset + 3]) << 24);
}

uint64_t heavy_hash(uint32_t value, int index) {
    uint64_t x = (static_cast<uint64_t>(value) << 32) ^ static_cast<uint32_t>(index);
    for (int i = 0; i < kHashRounds; ++i) {
        x += 0x9e3779b97f4a7c15ULL;
        x = (x ^ (x >> 30)) * 0xbf58476d1ce4e5b9ULL;
        x = (x ^ (x >> 27)) * 0x94d049bb133111ebULL;
        x ^= x >> 31;
    }
    return x;
}

void append_match(char* out, int cap, int& len, int index, uint32_t input, uint64_t hash) {
    if (len >= cap) {
        return;
    }
    const int n = std::snprintf(
        out + len,
        cap - len,
        "%d,%u,%016llx;",
        index,
        input,
        static_cast<unsigned long long>(hash));
    if (n > 0 && n < cap - len) {
        len += n;
    }
}
}  // namespace

WORKFLOW_JSON_NODE(64 * 1024, 96 * 1024)

int workflow_run_json(const char* input, int input_len, char* output, int output_cap, int& output_len) {
    const int range_start = static_cast<int>(runtime_json::extract_named_int(input, input_len, "range_start", 0));
    const int range_count = static_cast<int>(runtime_json::extract_named_int(input, input_len, "range_count", 250000));
    if (range_start < 0 || range_count <= 0 || range_start + range_count > kNumberCount) {
        return 2;
    }

    char url[kURLCap];
    const runtime_artifacts::BytesArtifact file =
        runtime_artifacts::fetch_bytes(input, input_len, "numbers-bin", g_numbers, kNumbersBytes, 10000, url, kURLCap);
    if (!file.ok || file.bytes != kNumbersBytes) {
        output_len = runtime_artifacts::write_fetch_error(output, output_cap, file);
        return 0;
    }

    int match_count = 0;
    int matches_len = 0;
    for (int i = 0; i < range_count; ++i) {
        const int index = range_start + i;
        const uint32_t number = read_u32_le(g_numbers, index * 4);
        const uint64_t hash = heavy_hash(number, index);
        if ((hash & kMatchMask) == 0) {
            ++match_count;
            append_match(g_matches, static_cast<int>(sizeof(g_matches)), matches_len, index, number, hash);
        }
    }
    g_matches[matches_len] = '\0';

    runtime_json::JsonWriter json(output, output_cap);
    json.begin_object();
    json.field("range_start", range_start);
    json.field("range_count", range_count);
    json.field("processed", range_count);
    json.field("match_count", match_count);
    json.field("matches", g_matches);
    output_len = json.end_object();
    return json.ok() ? 0 : 3;
}
