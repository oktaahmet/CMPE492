#include <string_view>

#include "../common/workflow.hpp"

namespace {
char ascii_lower(char c) {
    if (c >= 'A' && c <= 'Z') {
        return static_cast<char>(c - 'A' + 'a');
    }
    return c;
}
}  // namespace

int count_letter(std::string_view text, char letter) {
    int count = 0;
    for (char c : text) {
        if (ascii_lower(c) == ascii_lower(letter)) {
            ++count;
        }
    }
    return count;
}

WORKFLOW_NODE(input, output) {
    const auto url = input.string("url");
    const char letter = input.char_("letter");

    const auto json = workflow::fetch_json(url);
    const auto text = json.string("extract");
    output.number(count_letter(text, letter));
}
