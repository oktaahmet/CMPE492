#include "../common/workflow.hpp"

WORKFLOW_NODE(input, output) {
    const long long combined =
        input.node("reduce-gaps").number("output") +
        input.node("reduce-stats").number("output");
    output.number(combined);
}
