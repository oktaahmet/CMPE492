#include <cstdio>
#include <cstring>
#include <string>

#include "../common/sha256.hpp"
#include "../common/workflow.hpp"

namespace {
void digest_hex(const nonce_hash::Digest& digest, char out[65]) {
    static constexpr char hex[] = "0123456789abcdef";
    for (int i = 0; i < 32; ++i) {
        out[i * 2] = hex[(digest.bytes[i] >> 4) & 0x0f];
        out[i * 2 + 1] = hex[digest.bytes[i] & 0x0f];
    }
    out[64] = '\0';
}

void hash_text(std::string_view text, char out[65]) {
    nonce_hash::Sha256 sha;
    sha.update(reinterpret_cast<const unsigned char*>(text.data()), text.size());
    digest_hex(sha.final(), out);
}
}  // namespace

WORKFLOW_NODE_WITH_CAPS(input, output, 512 * 1024, 512) {
    const int shard_count = input.int_("shard_count", 200);
    const std::string target(input.string("target_hash"));
    if (shard_count <= 0 || shard_count > 10000 || target.size() != 64) {
        output.fail(30);
        return;
    }

    int found = 0;
    int winning_shard = -1;
    long long index = -1;
    long long attempts = 0;
    std::string candidate;
    char hash[65] = {};

    // The shard ids the runtime synthesized from `shard_count: 200` follow
    // the "<parent-id>-<i>" naming convention. We walk them by id rather
    // than asking the runtime how many there are — the workflow JSON's
    // `shard_count` is the source of truth and we just iterate up to it.
    // input.optional_node returns an empty Object when the id is absent,
    // which means the workflow JSON shrank without us updating the reducer.
    for (int i = 0; i < shard_count; ++i) {
        char node_id[40];
        std::snprintf(node_id, sizeof(node_id), "bruteforce-shard-%d", i);
        const auto shard = input.optional_node(node_id);
        if (!shard.ok()) {
            output.fail(31);
            return;
        }

        attempts += shard.number("attempts", 0);
        if (found != 0 || shard.int_("found", 0) == 0) {
            continue;
        }

        candidate = std::string(shard.string("candidate"));
        hash_text(candidate, hash);
        if (target == hash) {
            found = 1;
            winning_shard = i;
            index = shard.number("index", -1);
        }
    }

    auto json = output.object();
    json.field("found", found);
    json.field("index", index);
    json.field("winning_shard", winning_shard);
    json.field("candidate", found != 0 ? candidate.c_str() : "");
    json.field("hash", found != 0 ? hash : "");
    json.field("attempts", attempts);
    json.done();
}
