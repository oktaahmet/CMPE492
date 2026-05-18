#include <fstream>
#include <string>

#include "../common/workflow.hpp"

namespace {
bool parse_int(const std::string& s, int& out) {
    if (s.empty()) {
        return false;
    }
    int sign = 1;
    size_t i = 0;
    if (s[i] == '-' || s[i] == '+') {
        if (s[i] == '-') sign = -1;
        ++i;
    }
    int value = 0;
    bool any = false;
    for (; i < s.size(); ++i) {
        if (s[i] < '0' || s[i] > '9') return false;
        value = value * 10 + (s[i] - '0');
        any = true;
    }
    if (!any) return false;
    out = value * sign;
    return true;
}
}  // namespace

WORKFLOW_NODE(input, output) {
    const std::string path(input.artifact("cities").string("path"));
    std::ifstream file(path);
    if (!file) {
        output.fail(30);
        return;
    }

    std::string line;
    if (!std::getline(file, line)) {
        output.fail(31);
        return;
    }

    int city_count = 0;
    int min_x = 1 << 30, max_x = -(1 << 30);
    int min_y = 1 << 30, max_y = -(1 << 30);

    while (std::getline(file, line)) {
        const auto c1 = line.find(',');
        if (c1 == std::string::npos) continue;
        const auto c2 = line.find(',', c1 + 1);
        if (c2 == std::string::npos) continue;

        int x = 0, y = 0;
        if (!parse_int(line.substr(c1 + 1, c2 - c1 - 1), x)) continue;
        if (!parse_int(line.substr(c2 + 1), y)) continue;

        if (x < min_x) min_x = x;
        if (x > max_x) max_x = x;
        if (y < min_y) min_y = y;
        if (y > max_y) max_y = y;
        ++city_count;
    }

    auto json = output.object();
    json.field("city_count", city_count);
    json.field("bbox_width", max_x - min_x);
    json.field("bbox_height", max_y - min_y);
    json.done();
}
