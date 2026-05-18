#include <algorithm>
#include <vector>

#include "../common/workflow.hpp"

namespace {
constexpr long long kDefaultLimit = 10000000000000LL;
constexpr long long kDefaultRangeSize = 1000000000LL;
constexpr int kSegmentOddCount = 1 << 20;

long long isqrt(long long n) {
    long long lo = 0;
    long long hi = 1;
    while (hi <= n / hi) {
        hi <<= 1;
    }
    while (lo + 1 < hi) {
        const long long mid = lo + (hi - lo) / 2;
        if (mid <= n / mid) {
            lo = mid;
        } else {
            hi = mid;
        }
    }
    return lo;
}

std::vector<int> base_primes(long long max_value) {
    const int limit = static_cast<int>(max_value);
    std::vector<unsigned char> composite(static_cast<size_t>(limit / 2 + 1), 0);
    std::vector<int> primes;
    primes.reserve(250000);
    primes.push_back(2);

    for (long long p = 3; p <= limit; p += 2) {
        if (composite[static_cast<size_t>(p / 2)]) {
            continue;
        }
        primes.push_back(static_cast<int>(p));
        if (p > limit / p) {
            continue;
        }
        for (long long multiple = p * p; multiple <= limit; multiple += 2 * p) {
            composite[static_cast<size_t>(multiple / 2)] = 1;
        }
    }
    return primes;
}

void accept_prime(
    long long value,
    long long& first_prime,
    long long& last_prime,
    long long& max_gap,
    long long& gap_left,
    long long& gap_right,
    long long& prime_count) {
    if (first_prime == 0) {
        first_prime = value;
    }
    if (last_prime != 0) {
        const long long gap = value - last_prime;
        if (gap > max_gap) {
            max_gap = gap;
            gap_left = last_prime;
            gap_right = value;
        }
    }
    last_prime = value;
    ++prime_count;
}
}  // namespace

WORKFLOW_NODE_WITH_CAPS(input, output, 4096, 512) {
    const int shard_index = input.int_("shard_index", 0);
    const long long range_size = input.number("range_size", kDefaultRangeSize);
    const long long limit = input.number("limit", kDefaultLimit);

    if (shard_index < 0 || range_size <= 0 || limit < 2) {
        output.fail(30);
        return;
    }

    const long long low = 1 + static_cast<long long>(shard_index) * range_size;
    if (low > limit) {
        output.fail(31);
        return;
    }
    const long long high = std::min(limit, low + range_size - 1);
    const std::vector<int> primes = base_primes(isqrt(high));

    std::vector<unsigned char> is_prime(static_cast<size_t>(kSegmentOddCount), 1);
    long long first_prime = 0;
    long long last_prime = 0;
    long long max_gap = 0;
    long long gap_left = 0;
    long long gap_right = 0;
    long long prime_count = 0;

    if (low <= 2 && high >= 2) {
        accept_prime(2, first_prime, last_prime, max_gap, gap_left, gap_right, prime_count);
    }

    long long segment_low = std::max(3LL, low);
    if ((segment_low & 1LL) == 0) {
        ++segment_low;
    }

    const long long segment_span = static_cast<long long>(kSegmentOddCount) * 2;
    for (; segment_low <= high; segment_low += segment_span) {
        long long segment_high = std::min(high, segment_low + segment_span - 2);
        if ((segment_high & 1LL) == 0) {
            --segment_high;
        }
        const int odd_count = static_cast<int>((segment_high - segment_low) / 2 + 1);
        std::fill(is_prime.begin(), is_prime.begin() + odd_count, 1);

        for (const int p_int : primes) {
            if (p_int == 2) {
                continue;
            }
            const long long p = p_int;
            if (p > segment_high / p) {
                break;
            }

            long long first_multiple = ((segment_low + p - 1) / p) * p;
            if (first_multiple < p * p) {
                first_multiple = p * p;
            }
            if ((first_multiple & 1LL) == 0) {
                first_multiple += p;
            }

            for (long long value = first_multiple; value <= segment_high; value += 2 * p) {
                is_prime[static_cast<size_t>((value - segment_low) / 2)] = 0;
            }
        }

        for (int i = 0; i < odd_count; ++i) {
            if (!is_prime[static_cast<size_t>(i)]) {
                continue;
            }
            accept_prime(
                segment_low + 2LL * i,
                first_prime,
                last_prime,
                max_gap,
                gap_left,
                gap_right,
                prime_count);
        }
    }

    auto json = output.object();
    json.field("first_prime", first_prime);
    json.field("last_prime", last_prime);
    json.field("max_gap", max_gap);
    json.field("gap_left", gap_left);
    json.field("gap_right", gap_right);
    json.field("prime_count", prime_count);
    json.done();
}
