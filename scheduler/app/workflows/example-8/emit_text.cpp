#include <cstdio>

#include "../common/workflow.hpp"

WORKFLOW_NODE(input, output) {
    const int repeat = input.int_("repeat", 8);

    char buf[4096];
    int pos = 0;
    for (int i = 0; i < repeat && pos + 32 < static_cast<int>(sizeof(buf)); ++i) {
        pos += std::snprintf(buf + pos, static_cast<int>(sizeof(buf)) - pos, "item_%d ", i);
    }
    output.text(std::string_view(buf, static_cast<size_t>(pos)));
}
