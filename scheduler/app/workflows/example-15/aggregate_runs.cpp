#include <cstring>
#include <fstream>
#include <string>
#include <string_view>
#include <vector>

#include "../common/workflow.hpp"

namespace {
long long sum_values(const std::vector<long long>& values) {
    long long sum = 0;
    for (long long value : values) {
        sum += value;
    }
    return sum;
}

std::vector<long long> concat(std::vector<long long> a, const std::vector<long long>& b) {
    a.insert(a.end(), b.begin(), b.end());
    return a;
}

int count_substring(std::string_view text, const char* needle) {
    const int needle_len = static_cast<int>(std::strlen(needle));
    int count = 0;
    for (size_t i = 0; i + static_cast<size_t>(needle_len) <= text.size(); ++i) {
        bool match = true;
        for (int j = 0; j < needle_len; ++j) {
            if (text[i + static_cast<size_t>(j)] != needle[j]) {
                match = false;
                break;
            }
        }
        if (match) {
            ++count;
        }
    }
    return count;
}
}  // namespace

WORKFLOW_NODE_WITH_CAPS(input, output, 1024 * 1024, 2048) {
    const std::string report_path(input.output_artifact("simulation-report").string("path"));

    const auto baseline_wait = input.node("simulate-baseline").numbers("avg_wait_ms");
    const auto stress_wait = input.node("simulate-stress").numbers("avg_wait_ms");
    const auto wait_values = concat(baseline_wait, stress_wait);
    const int wait_count = static_cast<int>(wait_values.size());

    const auto system_values = concat(
        input.node("simulate-baseline").numbers("avg_system_ms"),
        input.node("simulate-stress").numbers("avg_system_ms"));
    const auto max_wait_values = concat(
        input.node("simulate-baseline").numbers("max_wait_ms"),
        input.node("simulate-stress").numbers("max_wait_ms"));
    const auto served_values = concat(
        input.node("simulate-baseline").numbers("served_customers"),
        input.node("simulate-stress").numbers("served_customers"));
    const auto event_values = concat(
        input.node("simulate-baseline").numbers("total_customer_events"),
        input.node("simulate-stress").numbers("total_customer_events"));

    const int avg_wait_ms = wait_count > 0 ? static_cast<int>(sum_values(wait_values) / wait_count) : 0;
    const int avg_system_ms = wait_count > 0 ? static_cast<int>(sum_values(system_values) / wait_count) : 0;
    const int max_wait_ms = static_cast<int>(sum_values(max_wait_values));
    const int served_total = static_cast<int>(sum_values(served_values));
    const int events_total = static_cast<int>(sum_values(event_values));
    const int source_bytes = input.node("fetch-public-signal").int_("source_bytes");
    const int profile_rows = input.node("read-load-profile").int_("profile_rows");
    const int profile_bytes = input.node("read-load-profile").int_("profile_bytes");
    const int trace_samples = count_substring(input.node("generate-audit-trace").view(), "trace_id=");

    std::ofstream report(report_path);
    if (!report) {
        output.fail(10);
        return;
    }
    report << "workflow=wf-full-system-capability-015\n";
    report << "simulation_samples=" << wait_count << "\n";
    report << "trace_samples=" << trace_samples << "\n";
    report << "avg_wait_ms=" << avg_wait_ms << "\n";
    report << "avg_system_ms=" << avg_system_ms << "\n";
    report << "max_wait_sum=" << max_wait_ms << "\n";
    report << "served_total=" << served_total << "\n";
    report << "events_total=" << events_total << "\n";
    report << "source_bytes=" << source_bytes << "\n";
    report << "profile_rows=" << profile_rows << "\n";
    report << "profile_bytes=" << profile_bytes << "\n";

    auto json = output.object();
    json.field("simulation_sample_count", wait_count);
    json.field("trace_sample_count", trace_samples);
    json.field("average_wait_ms", avg_wait_ms);
    json.field("average_system_ms", avg_system_ms);
    json.field("served_total", served_total);
    json.field("events_total", events_total);
    json.field("source_bytes", source_bytes);
    json.field("profile_rows", profile_rows);
    json.field("profile_bytes", profile_bytes);
    json.field("report", "simulation_report.txt");
    json.done();
}
