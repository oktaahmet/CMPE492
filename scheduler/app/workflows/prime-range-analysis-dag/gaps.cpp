#include "../common/workflow.hpp"

namespace {
bool is_prime(int value) {
    if (value < 2) {
        return false;
    }
    if (value == 2) {
        return true;
    }
    if (value % 2 == 0) {
        return false;
    }
    for (int i = 3; static_cast<long long>(i) * i <= value; i += 2) {
        if (value % i == 0) {
            return false;
        }
    }
    return true;
}

int max_prime_gap(int start, int end) {
    int previous = -1;
    int max_gap = 0;
    for (int value = start; value <= end; ++value) {
        if (!is_prime(value)) {
            continue;
        }
        if (previous >= 0 && value - previous > max_gap) {
            max_gap = value - previous;
        }
        previous = value;
    }
    return max_gap;
}
}  // namespace

WORKFLOW_NODE(input, output) {
    output.number(max_prime_gap(input.int_("start", 1), input.int_("end", 1)));
}
