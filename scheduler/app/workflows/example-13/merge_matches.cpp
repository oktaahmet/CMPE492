#include <cstdio>
#include <fstream>
#include <string>

#include "../common/workflow.hpp"

WORKFLOW_NODE_WITH_CAPS(input, output, 512 * 1024, 1024) {
    const std::string report_path(input.output_artifact("matches").string("path"));
    std::ofstream report(report_path);
    if (!report) {
        output.fail(30);
        return;
    }

    report << "index,input,hash\n";

    int shard_count = 0;
    int processed_total = 0;
    int reported_matches = 0;
    int written_matches = 0;

    // Iterate over hash-shard-0..N until a shard id stops appearing in inputs.
    // The workflow JSON pins shard ids; finding a gap means we've seen them all.
    for (int shard_idx = 0; shard_idx < 64; ++shard_idx) {
        char node_id[64];
        std::snprintf(node_id, sizeof(node_id), "hash-shard-%d", shard_idx);

        const auto shard = input.optional_node(node_id);
        if (!shard.ok()) {
            break;
        }

        ++shard_count;
        processed_total += shard.int_("processed", 0);
        reported_matches += shard.int_("match_count", 0);

        const auto matches = shard.string("matches");
        for (const char c : matches) {
            if (c == ';') {
                report << '\n';
                ++written_matches;
            } else {
                report << c;
            }
        }
    }

    auto json = output.object();
    json.field("shard_count", shard_count);
    json.field("processed_total", processed_total);
    json.field("reported_matches", reported_matches);
    json.field("written_matches", written_matches);
    json.field("report", "matches.txt");
    json.done();
}
