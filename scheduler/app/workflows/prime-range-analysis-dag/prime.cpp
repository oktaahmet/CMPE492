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

int count_primes_in_range(int start, int end) {
    if (end < start) {
        return 0;
    }
    int count = 0;
    for (int value = start; value <= end; ++value) {
        if (is_prime(value)) {
            ++count;
        }
    }
    return count;
}
}  // namespace

WORKFLOW_NODE(input, output) {
    output.number(count_primes_in_range(input.int_("start", 1), input.int_("end", 1)));
}
