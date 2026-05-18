#include <cstdio>
#include <fstream>
#include <string>
#include <string_view>
#include <vector>

#include "../common/workflow.hpp"

namespace {
std::vector<std::string> load_city_names(const std::string& path) {
    std::vector<std::string> names;
    std::ifstream file(path);
    if (!file) return names;
    std::string line;
    if (!std::getline(file, line)) return names;  // header
    while (std::getline(file, line)) {
        const auto comma = line.find(',');
        if (comma == std::string::npos) continue;
        names.push_back(line.substr(0, comma));
    }
    return names;
}

std::vector<int> parse_tour(std::string_view tour) {
    std::vector<int> out;
    int value = 0;
    bool reading = false;
    for (size_t i = 0; i <= tour.size(); ++i) {
        const char c = i < tour.size() ? tour[i] : ',';
        if (c >= '0' && c <= '9') {
            value = value * 10 + (c - '0');
            reading = true;
        } else if (c == ',') {
            if (reading) out.push_back(value);
            value = 0;
            reading = false;
        }
    }
    return out;
}
}  // namespace

WORKFLOW_NODE_WITH_CAPS(input, output, 16 * 1024, 1024) {
    const int best_length = input.node("verify-best").int_("best_length", -1);
    const int shards_at_best = input.node("verify-best").int_("shards_at_best", 0);
    if (best_length < 0) {
        output.fail(30);
        return;
    }

    // Locate the shard that found the best tour. Shards are independent jobs,
    // so we walk by id pattern and stop at the first gap.
    std::string_view chosen_tour;
    int chosen_seed = -1;
    int total_shards = 0;
    for (int idx = 0; idx < 256; ++idx) {
        char node_id[32];
        std::snprintf(node_id, sizeof(node_id), "search-shard-%d", idx);
        const auto shard = input.optional_node(node_id);
        if (!shard.ok()) break;
        ++total_shards;
        if (chosen_tour.empty() && shard.int_("tour_length", -1) == best_length) {
            chosen_tour = shard.string("tour");
            chosen_seed = shard.int_("seed_offset", -1);
        }
    }
    if (chosen_tour.empty()) {
        output.fail(31);
        return;
    }

    const auto tour_indices = parse_tour(chosen_tour);
    const auto city_names = load_city_names(std::string(input.artifact("cities").string("path")));

    const std::string report_path(input.output_artifact("solution").string("path"));
    std::ofstream report(report_path);
    if (!report) {
        output.fail(32);
        return;
    }

    report << "best_length_sq=" << best_length << "\n";
    report << "found_by_seed=" << chosen_seed << "\n";
    report << "shards_at_best=" << shards_at_best << "/" << total_shards << "\n";
    report << "tour=\n";
    for (size_t i = 0; i < tour_indices.size(); ++i) {
        const int idx = tour_indices[i];
        report << (i == 0 ? "  " : "  -> ");
        if (idx >= 0 && static_cast<size_t>(idx) < city_names.size()) {
            report << city_names[idx];
        } else {
            report << "city_" << idx;
        }
        report << "\n";
    }

    auto json = output.object();
    json.field("best_length", best_length);
    json.field("found_by_seed", chosen_seed);
    json.field("shards_at_best", shards_at_best);
    json.field("total_shards", total_shards);
    json.field("city_count", static_cast<int>(tour_indices.size()));
    json.field("report", "tour_solution.txt");
    json.done();
}
