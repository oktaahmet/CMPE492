#include "../common/workflow.hpp"

WORKFLOW_NODE_WITH_CAPS(input, output, 64 * 1024, 1024) {
    const int expected_score_samples = input.int_("expected_score_samples", 3);

    // collect-metrics is a single consensus result, so reading the field directly is enough.
    const int source_sample_count = input.node("collect-metrics").int_("sample_count", 0);

    // score-metrics uses collect_all; its node value contains every replica
    // submission, each carrying its own "score" field. numbers() walks that
    // wrapped payload and returns one entry per submission.
    const auto scores = input.node("score-metrics").numbers("score");
    const int score_count = static_cast<int>(scores.size());
    long long score_total = 0;
    for (const long long value : scores) {
        score_total += value;
    }
    const int score_average = score_count > 0 ? static_cast<int>(score_total / score_count) : 0;

    auto json = output.object();
    json.field("source_sample_count", source_sample_count);
    json.field("score_sample_count", score_count);
    json.field("expected_score_samples", expected_score_samples);
    json.field("score_average", score_average);
    json.field("complete", score_count == expected_score_samples);
    json.field("review", score_average >= 760 ? "manual_review" : "auto_accept");
    json.done();
}
