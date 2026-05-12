#include <cstdio>
#include <cstring>

#include "../common/workflow.hpp"

namespace {
int count_token(std::string_view data, const char* token) {
    const int token_len = static_cast<int>(std::strlen(token));
    if (token_len <= 0 || data.size() < static_cast<size_t>(token_len)) {
        return 0;
    }

    int count = 0;
    for (size_t i = 0; i + static_cast<size_t>(token_len) <= data.size(); ++i) {
        if (std::memcmp(data.data() + i, token, static_cast<size_t>(token_len)) == 0) {
            ++count;
            i += static_cast<size_t>(token_len - 1);
        }
    }
    return count;
}
}  // namespace

WORKFLOW_NODE(input, output) {
    const int passes = input.int_("passes", 2);
    const std::string_view upstream = input.node("emit-text").string("output");
    const int item_count = count_token(upstream, "item_");

    char buf[256];
    const int n = std::snprintf(
        buf,
        static_cast<int>(sizeof(buf)),
        "passes_%d bytes_%d items_%d",
        passes,
        static_cast<int>(upstream.size()),
        item_count);
    if (n < 0 || n >= static_cast<int>(sizeof(buf))) {
        output.fail(30);
        return;
    }
    output.text(std::string_view(buf, static_cast<size_t>(n)));
}
