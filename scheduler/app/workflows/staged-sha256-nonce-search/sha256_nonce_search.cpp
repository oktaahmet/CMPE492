#include <cstdio>
#include <string>

#include "../common/sha256.hpp"
#include "../common/workflow.hpp"

namespace {
std::string message_for(workflow::Input& input) {
    const int source_round = input.int_("source_round", 0);
    if (source_round <= 0) {
        return std::string(input.string("message"));
    }

    char node_id[32];
    std::snprintf(node_id, sizeof(node_id), "verify-round-%d", source_round);
    return std::string(input.node(node_id).string("message"));
}
}

WORKFLOW_NODE(input, output) {
    const int round = input.int_("round", 1);
    const long long start = input.number("start");
    const long long attempts = input.number("attempts");
    const int target_bits = input.int_("target_bits", 16);
    const std::string message = message_for(input);

    int found = 0;
    long long nonce = 0;
    long long best_nonce = start;
    int best_bits = -1;
    long long attempts_done = 0;

    for (long long i = 0; i < attempts; ++i) {
        const long long current_nonce = start + i;
        const nonce_hash::Digest digest = nonce_hash::hash_nonce(message, current_nonce);
        const int bits = nonce_hash::leading_zero_bits(digest);
        attempts_done = i + 1;

        if (bits > best_bits) {
            best_bits = bits;
            best_nonce = current_nonce;
        }
        if (bits >= target_bits) {
            found = 1;
            nonce = current_nonce;
            best_nonce = current_nonce;
            best_bits = bits;
            break;
        }
    }

    auto json = output.object();
    json.field("round", round);
    json.field("found", found);
    json.field("nonce", nonce);
    json.field("best_nonce", best_nonce);
    json.field("best_bits", best_bits);
    json.field("attempts", attempts_done);
    json.field("target_bits", target_bits);
    json.done();
}
