#include <cstdio>

#include "../common/workflow.hpp"

WORKFLOW_NODE(input, output) {
    const int repeat = input.int_("repeat", 8);

    char buf[4096];
    int pos = 0;
    buf[pos++] = '"';
    for (int i = 0; i < repeat && pos + 32 < static_cast<int>(sizeof(buf)) - 2; ++i) {
        pos += std::snprintf(buf + pos, static_cast<int>(sizeof(buf)) - pos - 2, "item_%d ", i);
    }
    buf[pos++] = '"';
    buf[pos] = '\0';
    output.text(buf);
}
