#include <fstream>

#include "../common/workflow.hpp"

namespace {
long long sum_values(const std::vector<long long>& values) {
    long long total = 0;
    for (const long long value : values) {
        total += value;
    }
    return total;
}
}

WORKFLOW_NODE(input, output) {
    const auto pi_worker = input.node("pi-worker");
    const long long inside = sum_values(pi_worker.numbers("inside"));
    const long long samples = sum_values(pi_worker.numbers("samples"));
    const long long pi_scaled = samples > 0 ? (4LL * inside * 1000000LL) / samples : 0;

    const auto report_path = input.output_artifact("pi-report").string("path");
    std::ofstream report{std::string(report_path)};
    if (!report) {
        output.fail(30);
        return;
    }

    report << "Monte Carlo pi\n";
    report << "samples=" << samples << "\n";
    report << "inside=" << inside << "\n";
    report << "pi_scaled=" << pi_scaled << "\n";

    auto json = output.object();
    json.field("samples", samples);
    json.field("inside", inside);
    json.field("pi_scaled", pi_scaled);
    json.field("report", "pi_report.txt");
    json.done();
}
