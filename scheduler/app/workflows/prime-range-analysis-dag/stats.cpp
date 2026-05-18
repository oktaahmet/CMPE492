#include "../common/workflow.hpp"

WORKFLOW_NODE(input, output) {
    const int start = input.int_("start", 1);
    const int end = input.int_("end", start);
    output.number(end >= start ? end - start + 1 : 0);
}
