#include "../common/workflow.hpp"

WORKFLOW_NODE(input, output) {
    const long long total =
        input.node("stats-a").number("output") +
        input.node("stats-b").number("output") +
        input.node("stats-c").number("output") +
        input.node("stats-d").number("output");
    output.number(total);
}
