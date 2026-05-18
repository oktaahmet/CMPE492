#include <cstdio>
#include <fstream>
#include <string>

#include "../common/workflow.hpp"

// Server-side reducer. The 512 KiB input cap is sized to hold ALL upstream
// shard outputs inlined in a single payload — server reducers don't go
// through the chunked dependency loader the browser worker uses; they
// receive the full payload via stdin (see common/native_json_runner.cpp).
WORKFLOW_NODE_WITH_CAPS(input, output, 512 * 1024, 1024) {
    // input.output_artifact("matches") returns the path the runtime reserved
    // for this node's declared output_artifacts entry. We write the file to
    // that exact path; the server records {url, size, sha256} in the
    // finalized output automatically after the node exits successfully.
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

    // Walk hash-shard-0..N. Stop at the first missing id (input.optional_node
    // returns empty when the id is absent). This makes the reducer invariant
    // to the exact shard count declared in the workflow JSON.
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
