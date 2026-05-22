#include "../common/workflow.hpp"

WORKFLOW_NODE(input, output) {
    const auto tuning = input.node("fetch-tuning-context");
    const auto cities = input.node("read-cities");

    const int city_count = cities.int_("city_count", 0);
    const int swap_passes = tuning.int_("swap_passes", 3);

    // Each search shard is sized to a bounded amount of work so it always
    // finishes within the WASM worker timeout. To do more total work, add
    // more shard nodes in the workflow JSON — do NOT make a single shard
    // longer than the worker timeout window.
    const int restarts_per_shard = input.int_("restarts_per_shard", 50000);

    auto json = output.object();
    json.field("city_count", city_count);
    json.field("restarts_per_shard", restarts_per_shard);
    json.field("swap_passes", swap_passes);
    json.done();
}
