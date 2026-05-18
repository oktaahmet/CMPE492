#include <cstdio>
#include <cstring>
#include <string>

#include "../common/sha256.hpp"
#include "../common/workflow.hpp"

namespace {
constexpr int kCandidateLen = 6;
constexpr long long kKeyspace = 308915776LL;  // 26^6

void digest_hex(const nonce_hash::Digest& digest, char out[65]) {
    static constexpr char hex[] = "0123456789abcdef";
    for (int i = 0; i < 32; ++i) {
        out[i * 2] = hex[(digest.bytes[i] >> 4) & 0x0f];
        out[i * 2 + 1] = hex[digest.bytes[i] & 0x0f];
    }
    out[64] = '\0';
}

void candidate_for(long long index, char out[kCandidateLen + 1]) {
    for (int i = kCandidateLen - 1; i >= 0; --i) {
        out[i] = static_cast<char>('a' + (index % 26));
        index /= 26;
    }
    out[kCandidateLen] = '\0';
}

void hash_text(const char* text, char out[65]) {
    nonce_hash::Sha256 sha;
    sha.update(reinterpret_cast<const unsigned char*>(text), std::strlen(text));
    digest_hex(sha.final(), out);
}
}  // namespace

WORKFLOW_NODE_WITH_CAPS(input, output, 4096, 512) {
    // This node was authored once in the workflow JSON with `shard_count: 200`.
    // The scheduler expanded it into bruteforce-shard-0 .. bruteforce-shard-199
    // at load time and injected `shard_index = i` into each child's args[0].
    // The C++ here is shard-count-agnostic: it just reads its own index and
    // walks its own slice of the keyspace.
    const int shard_index = input.int_("shard_index", 0);
    const long long range_size = input.number("range_size", 1544579);
    const std::string target(input.string("target_hash"));
    if (shard_index < 0 || range_size <= 0 || target.size() != 64) {
        output.fail(30);
        return;
    }

    const long long start = static_cast<long long>(shard_index) * range_size;
    const long long end = start + range_size < kKeyspace ? start + range_size : kKeyspace;
    char candidate[kCandidateLen + 1] = {};
    char hash[65] = {};
    char found_candidate[kCandidateLen + 1] = {};
    char found_hash[65] = {};
    int found = 0;
    long long found_index = -1;
    long long attempts = 0;

    for (long long index = start; index < end; ++index) {
        candidate_for(index, candidate);
        hash_text(candidate, hash);
        attempts += 1;
        if (target == hash) {
            found = 1;
            found_index = index;
            std::snprintf(found_candidate, sizeof(found_candidate), "%s", candidate);
            std::snprintf(found_hash, sizeof(found_hash), "%s", hash);
            break;
        }
    }

    auto json = output.object();
    json.field("found", found);
    json.field("index", found_index);
    json.field("candidate", found_candidate);
    json.field("hash", found_hash);
    json.field("attempts", attempts);
    json.done();
}
