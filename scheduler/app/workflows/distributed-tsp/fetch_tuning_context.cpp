#include <cstring>

#include "../common/workflow.hpp"

namespace {
constexpr const char* kURL =
    "https://en.wikipedia.org/w/api.php?action=query&format=json&origin=*"
    "&titles=Travelling_salesman_problem&prop=extracts&exintro=1&explaintext=1";

int count_substring(std::string_view data, const char* needle) {
    const int nlen = static_cast<int>(std::strlen(needle));
    if (nlen <= 0 || static_cast<int>(data.size()) < nlen) {
        return 0;
    }
    int count = 0;
    for (int i = 0; i + nlen <= static_cast<int>(data.size()); ++i) {
        if (std::memcmp(data.data() + i, needle, static_cast<size_t>(nlen)) == 0) {
            ++count;
            i += nlen - 1;
        }
    }
    return count;
}
}  // namespace

WORKFLOW_NODE_WITH_CAPS(input, output, 2048, 512) {
    const int timeout_ms = input.int_("timeout_ms", 3000);

    char body[12 * 1024];
    const auto resp = workflow::fetch_text(kURL, body, sizeof(body), timeout_ms);

    // Derive search effort from real-world signal; fall back to neutral defaults
    // so the workflow keeps making progress when the fetch is unavailable.
    const std::string_view text(body, resp.ok ? static_cast<size_t>(resp.bytes) : 0);
    const int restarts_per_worker = resp.ok ? 120 + count_substring(text, "tour") * 4 : 120;
    const int swap_passes = resp.ok ? 3 + (count_substring(text, "algorithm") % 3) : 3;

    auto json = output.object();
    json.field("fetched", resp.ok);
    json.field("restarts_per_worker", restarts_per_worker);
    json.field("swap_passes", swap_passes);
    json.done();
}
