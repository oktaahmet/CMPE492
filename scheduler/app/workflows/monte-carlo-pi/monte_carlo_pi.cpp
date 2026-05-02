#include "../common/workflow.hpp"

WORKFLOW_NODE(input, output) {
    const long long samples = input.number("samples", 200000);
    long long inside = 0;

    for (long long i = 0; i < samples; ++i) {
        const unsigned int x = workflow::random_u32() & 0xFFFFFFu;
        const unsigned int y = workflow::random_u32() & 0xFFFFFFu;
        const unsigned long long xx = static_cast<unsigned long long>(x) * x;
        const unsigned long long yy = static_cast<unsigned long long>(y) * y;
        const unsigned long long rr = 0xFFFFFFull * 0xFFFFFFull;
        if (xx + yy <= rr) {
            ++inside;
        }
    }

    const long long pi_scaled = samples > 0 ? (4LL * inside * 1000000LL) / samples : 0;

    auto json = output.object();
    json.field("samples", samples);
    json.field("inside", inside);
    json.field("pi_scaled", pi_scaled);
    json.done();
}
