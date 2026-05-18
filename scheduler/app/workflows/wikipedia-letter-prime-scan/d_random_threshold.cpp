#include "../common/workflow.hpp"

WORKFLOW_NODE(input, output) {
    const long long a = input.node("A").number("output");
    const long long b = input.node("B").number("output");
    const long long c = input.node("C").number("output");
    const long long k = (a * b + c) * 1000LL;

    long long accepted_sum = 0;
    long long accepted_count = 0;
    for (int i = 0; i < 10000; ++i) {
        const long long random_number = workflow::random_between(1, 1000000000);
        if (random_number < k) {
            accepted_sum += random_number;
            ++accepted_count;
        }
    }

    const long long average = accepted_count > 0 ? (accepted_sum + accepted_count / 2LL) / accepted_count : 0;
    output.number(average);
}
