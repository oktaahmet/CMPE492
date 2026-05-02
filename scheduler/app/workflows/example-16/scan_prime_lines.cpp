#include <string_view>
#include <vector>

#include "../common/workflow.hpp"

namespace {
int parse_numbers_by_line(const char* data, int len, std::vector<int>& out) {
    int value = 0;
    bool in_number = false;
    for (int i = 0; i <= len; ++i) {
        const char c = i < len ? data[i] : '\n';
        if (c >= '0' && c <= '9') {
            value = value * 10 + static_cast<int>(c - '0');
            in_number = true;
            continue;
        }
        if (c == '\n' || c == '\r') {
            if (in_number) {
                out.push_back(value);
            }
            value = 0;
            in_number = false;
        }
    }
    return static_cast<int>(out.size());
}

bool is_prime(int value) {
    if (value < 2) {
        return false;
    }
    for (int divisor = 2; divisor * divisor <= value; ++divisor) {
        if (value % divisor == 0) {
            return false;
        }
    }
    return true;
}
}  // namespace

WORKFLOW_NODE_WITH_CAPS(input, output, 65536, 2 * 1024 * 1024) {
    const std::string_view parity = input.string("line_parity");
    const bool want_odd = parity == "odd";

    static char body[16 * 1024 * 1024];
    const auto artifact = input.artifact("numbers").fetch_text(body, static_cast<int>(sizeof(body)));

    std::vector<int> numbers;
    numbers.reserve(1024);
    parse_numbers_by_line(body, artifact.bytes, numbers);

    std::vector<int> primes;
    primes.reserve(1024);
    for (size_t i = 0; i < numbers.size(); i += 1) {
        const int line_number = static_cast<int>(i) + 1;
        const bool is_odd_line = (line_number % 2) != 0;
        if (is_odd_line != want_odd) {
            continue;
        }
        const int value = numbers[i];
        if (is_prime(value)) {
            primes.push_back(value);
        }
    }

    output.array(primes);
}
