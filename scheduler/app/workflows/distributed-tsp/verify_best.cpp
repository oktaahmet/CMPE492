#include <cstdio>

#include "../common/workflow.hpp"

WORKFLOW_NODE_WITH_CAPS(input, output, 16 * 1024, 512) {
    long long sum = 0;
    long long best = (1LL << 60);
    long long worst = 0;
    int sample_count = 0;

    // Walk search-shard-0..N until a gap tells us we've enumerated every shard.
    // verify-best does not need to know the shard count; the workflow JSON pins it.
    for (int idx = 0; idx < 256; ++idx) {
        char node_id[32];
        std::snprintf(node_id, sizeof(node_id), "search-shard-%d", idx);

        const auto shard = input.optional_node(node_id);
        if (!shard.ok()) {
            break;
        }
        const long long len = shard.int_("tour_length", -1);
        if (len < 0) continue;

        sum += len;
        if (len < best) best = len;
        if (len > worst) worst = len;
        ++sample_count;
    }

    if (sample_count == 0) {
        output.fail(30);
        return;
    }

    int shards_at_best = 0;
    for (int idx = 0; idx < 256; ++idx) {
        char node_id[32];
        std::snprintf(node_id, sizeof(node_id), "search-shard-%d", idx);
        const auto shard = input.optional_node(node_id);
        if (!shard.ok()) break;
        if (shard.int_("tour_length", -1) == static_cast<int>(best)) ++shards_at_best;
    }

    auto json = output.object();
    json.field("shards", sample_count);
    json.field("best_length", static_cast<int>(best));
    json.field("worst_length", static_cast<int>(worst));
    json.field("avg_length", static_cast<int>(sum / static_cast<long long>(sample_count)));
    json.field("spread", static_cast<int>(worst - best));
    json.field("shards_at_best", shards_at_best);
    json.done();
}
