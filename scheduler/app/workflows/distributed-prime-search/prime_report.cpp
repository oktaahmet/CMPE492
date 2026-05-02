#include <fstream>
#include <vector>

#include "../common/workflow.hpp"

namespace {
void append_primes(const workflow::Input& input, const char* node_id, std::vector<int>& primes) {
    const std::vector<int> shard = input.node(node_id).number_array("output");
    primes.insert(primes.end(), shard.begin(), shard.end());
}
}  // namespace

WORKFLOW_NODE_WITH_CAPS(input, output, 8 * 1024 * 1024, 4096) {
    std::vector<int> primes;
    primes.reserve(360000);
    append_primes(input, "prime-1", primes);
    append_primes(input, "prime-2", primes);
    append_primes(input, "prime-3", primes);
    append_primes(input, "prime-4", primes);
    append_primes(input, "prime-5", primes);

    const auto output_path = input.output_artifact("prime-list").string("path");
    std::ofstream file{std::string(output_path)};
    if (!file) {
        output.fail(30);
        return;
    }

    for (const int prime : primes) {
        file << prime << '\n';
    }

    auto json = output.object();
    json.field("range_start", 1);
    json.field("range_end", 5000000);
    json.field("prime_count", static_cast<long long>(primes.size()));
    json.field("first_prime", primes.empty() ? 0 : primes.front());
    json.field("last_prime", primes.empty() ? 0 : primes.back());
    json.field("artifact", "primes.txt");
    json.done();
}
