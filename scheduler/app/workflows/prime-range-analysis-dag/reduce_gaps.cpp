#include "../common/workflow.hpp"

WORKFLOW_NODE(input, output) {
    const long long total =
        input.node("gaps-a").number("output") +
        input.node("gaps-b").number("output") +
        input.node("gaps-c").number("output") +
        input.node("gaps-d").number("output");
    output.number(total);
}
