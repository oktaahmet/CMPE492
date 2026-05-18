#include <fstream>

#include "../common/workflow.hpp"

WORKFLOW_NODE_WITH_CAPS(input, output, 262144, 16) {
    const std::string path(input.output_artifact("numbers").string("path"));
    const std::vector<long long> values = input.node("D").numbers("output");

    long long sum = 0;
    for (long long value : values) {
        sum += value;
    }
    const long long line_count = (sum + static_cast<long long>(values.size()) / 2LL) / static_cast<long long>(values.size());

    std::ofstream file(path);
    if (!file) {
        output.fail(10);
        return;
    }
    for (long long i = 1; i <= line_count; ++i) {
        file << i << '\n';
    }

    output.null();
}
