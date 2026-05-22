#include <algorithm>
#include <cstdint>
#include <vector>

#include "../common/workflow.hpp"

namespace {
constexpr long long kDefaultLimit = 2000000000LL;
constexpr long long kDefaultRangeSize = 10000000LL;
constexpr int kSegmentOddCount = 1 << 20;
constexpr char kBase64[] = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";

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
    primes.reserve(5000);
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

void append_u32_le(std::vector<unsigned char>& out, uint32_t value) {
    out.push_back(static_cast<unsigned char>(value & 0xffU));
    out.push_back(static_cast<unsigned char>((value >> 8) & 0xffU));
    out.push_back(static_cast<unsigned char>((value >> 16) & 0xffU));
    out.push_back(static_cast<unsigned char>((value >> 24) & 0xffU));
}

void emit_base64(const std::vector<unsigned char>& bytes, workflow::Output& output) {
    static char encoded[4 * 1024 * 1024];
    int pos = 0;

    for (size_t i = 0; i < bytes.size(); i += 3) {
        const uint32_t b0 = bytes[i];
        const uint32_t b1 = i + 1 < bytes.size() ? bytes[i + 1] : 0;
        const uint32_t b2 = i + 2 < bytes.size() ? bytes[i + 2] : 0;
        if (pos + 4 >= static_cast<int>(sizeof(encoded))) {
            output.fail(32);
            return;
        }
        encoded[pos++] = kBase64[(b0 >> 2) & 0x3f];
        encoded[pos++] = kBase64[((b0 & 0x03) << 4) | ((b1 >> 4) & 0x0f)];
        encoded[pos++] = i + 1 < bytes.size() ? kBase64[((b1 & 0x0f) << 2) | ((b2 >> 6) & 0x03)] : '=';
        encoded[pos++] = i + 2 < bytes.size() ? kBase64[b2 & 0x3f] : '=';
    }

    output.text(std::string_view(encoded, static_cast<size_t>(pos)));
}
}  // namespace

WORKFLOW_NODE_WITH_CAPS(input, output, 4096, 4 * 1024 * 1024) {
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

    std::vector<unsigned char> prime_bytes;
    prime_bytes.reserve(3000000);
    std::vector<unsigned char> is_prime(static_cast<size_t>(kSegmentOddCount), 1);

    if (low <= 2 && high >= 2) {
        append_u32_le(prime_bytes, 2);
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
            append_u32_le(prime_bytes, static_cast<uint32_t>(segment_low + 2LL * i));
        }
    }

    emit_base64(prime_bytes, output);
}
