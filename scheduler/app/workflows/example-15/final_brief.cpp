#include <fstream>
#include <string>

#include "../common/workflow.hpp"

WORKFLOW_NODE_WITH_CAPS(input, output, 131072, 1536) {
    const std::string title(input.string("title"));
    const std::string brief_path(input.output_artifact("final-brief").string("path"));
    const auto aggregate = input.node("aggregate-simulations");

    const int average_wait_ms = aggregate.int_("average_wait_ms");
    const int simulation_sample_count = aggregate.int_("simulation_sample_count");
    const int trace_sample_count = aggregate.int_("trace_sample_count");
    const int score = input.node("score-policy").int_("output");

    std::ofstream brief(brief_path);
    if (!brief) {
        output.fail(10);
        return;
    }
    brief << title << "\n";
    brief << "simulation_sample_count=" << simulation_sample_count << "\n";
    brief << "trace_sample_count=" << trace_sample_count << "\n";
    brief << "average_wait_ms=" << average_wait_ms << "\n";
    brief << "policy_score=" << score << "\n";
    brief << "notes=browser fetch, artifact fetch, collect_all, consensus, server reducer, large output, and output artifacts all ran in one DAG\n";

    auto json = output.object();
    json.field("simulation_sample_count", simulation_sample_count);
    json.field("trace_sample_count", trace_sample_count);
    json.field("average_wait_ms", average_wait_ms);
    json.field("policy_score", score);
    json.field("brief", "final_brief.txt");
    json.done();
}
