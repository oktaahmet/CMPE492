#include <algorithm>
#include <fstream>
#include <vector>

#include "../common/workflow.hpp"

WORKFLOW_NODE_WITH_CAPS(input, output, 4 * 1024 * 1024, 16) {
    const std::string path(input.output_artifact("final").string("path"));

    std::vector<int> primes = input.node("G").number_array("output");
    const std::vector<int> h_primes = input.node("H").number_array("output");
    primes.insert(primes.end(), h_primes.begin(), h_primes.end());
    std::sort(primes.begin(), primes.end());
    primes.erase(std::unique(primes.begin(), primes.end()), primes.end());

    std::ofstream file(path);
    if (!file) {
        output.fail(10);
        return;
    }
    for (int value : primes) {
        file << value << '\n';
    }

    output.null();
}
