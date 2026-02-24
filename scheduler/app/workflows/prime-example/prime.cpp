#include <emscripten/emscripten.h>

static int is_prime(int value) {
    if (value < 2) {
        return 0;
    }
    if (value == 2) {
        return 1;
    }
    if (value % 2 == 0) {
        return 0;
    }
    for (int i = 3; (long long)i * i <= value; i += 2) {
        if (value % i == 0) {
            return 0;
        }
    }
    return 1;
}

static int count_primes_in_range(int start, int end) {
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

extern "C" {
EMSCRIPTEN_KEEPALIVE int run(int start, int end) {
    return count_primes_in_range(start, end);
}
}
