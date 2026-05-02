#include "../common/workflow.hpp"

#include <vector>

namespace {
bool is_prime(long long value) {
    if (value < 2) {
        return false;
    }
    if (value == 2) {
        return true;
    }
    if (value % 2 == 0) {
        return false;
    }
    for (long long divisor = 3; divisor * divisor <= value; divisor += 2) {
        if (value % divisor == 0) {
            return false;
        }
    }
    return true;
}
}

WORKFLOW_NODE_WITH_CAPS(input, output, 4096, 1024 * 1024) {
    const int start = input.int_("start");
    const int end = input.int_("end");
    std::vector<int> primes;
    primes.reserve(80000);

    for (int value = start; value <= end; ++value) {
        if (!is_prime(value)) {
            continue;
        }
        primes.push_back(value);
    }

    output.array(primes);
}
