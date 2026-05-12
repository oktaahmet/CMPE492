#include <cstdio>

#include "../common/workflow.hpp"

WORKFLOW_NODE_WITH_CAPS(input, output, 10 * 1024 * 1024, 512) {
    const int shard_count = input.int_("shard_count", 10000);
    if (shard_count <= 0 || shard_count > 10000) {
        output.fail(30);
        return;
    }

    long long total_primes = 0;
    long long previous_last = 0;
    long long max_gap = 0;
    long long gap_left = 0;
    long long gap_right = 0;
    int winning_shard = -1;

    for (int i = 0; i < shard_count; ++i) {
        char node_id[40];
        std::snprintf(node_id, sizeof(node_id), "prime-gap-shard-%d", i);
        const auto shard = input.optional_node(node_id);
        if (!shard.ok()) {
            output.fail(31);
            return;
        }

        const long long first_prime = shard.number("first_prime", 0);
        const long long last_prime = shard.number("last_prime", 0);
        const long long shard_gap = shard.number("max_gap", 0);
        const long long shard_left = shard.number("gap_left", 0);
        const long long shard_right = shard.number("gap_right", 0);
        total_primes += shard.number("prime_count", 0);

        if (shard_gap > max_gap) {
            max_gap = shard_gap;
            gap_left = shard_left;
            gap_right = shard_right;
            winning_shard = i;
        }

        if (previous_last != 0 && first_prime != 0) {
            const long long boundary_gap = first_prime - previous_last;
            if (boundary_gap > max_gap) {
                max_gap = boundary_gap;
                gap_left = previous_last;
                gap_right = first_prime;
                winning_shard = i;
            }
        }

        if (last_prime != 0) {
            previous_last = last_prime;
        }
    }

    auto json = output.object();
    json.field("max_gap", max_gap);
    json.field("gap_left", gap_left);
    json.field("gap_right", gap_right);
    json.field("composite_run_start", gap_left + 1);
    json.field("composite_run_end", gap_right - 1);
    json.field("composite_run_length", max_gap > 0 ? max_gap - 1 : 0);
    json.field("winning_shard", winning_shard);
    json.field("prime_count", total_primes);
    json.done();
}
