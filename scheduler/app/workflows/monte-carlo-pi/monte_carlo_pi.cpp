#include "../common/workflow.hpp"

// Pi estimation via Monte Carlo sampling. This node is intentionally
// random — every replica produces a slightly different `inside` count.
// That is why the workflow JSON pins this node to:
//   "replication_factor": 3, "acceptance_policy": "collect_all"
// With `consensus` the replicas would never agree byte-for-byte and the
// job would never finalize. `collect_all` keeps every replica's output
// for the downstream pi_average reducer to merge.
WORKFLOW_NODE(input, output) {
    const long long samples = input.number("samples", 200000);
    long long inside = 0;

    for (long long i = 0; i < samples; ++i) {
        // workflow::random_u32() pulls 4 bytes from the host RNG:
        // wasi_snapshot_preview1.random_get in the browser, std::random_device
        // on a server build. There is no PRNG state to seed manually.
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
