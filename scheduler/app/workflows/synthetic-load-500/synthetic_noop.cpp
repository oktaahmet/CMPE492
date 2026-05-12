#include "../common/workflow.hpp"

WORKFLOW_NODE(input, output) {
    auto json = output.object();
    json.field("synthetic", true);
    json.done();
}
