#include <cstdint>
#include <cstdio>

#include "../common/workflow.hpp"

namespace {
constexpr int kMaxCities = 64;

struct City {
    int x;
    int y;
};

int dist_sq(const City& a, const City& b) {
    const int dx = a.x - b.x;
    const int dy = a.y - b.y;
    return dx * dx + dy * dy;
}

int tour_length(const City* cities, const int* order, int n) {
    int total = 0;
    for (int i = 0; i < n; ++i) {
        total += dist_sq(cities[order[i]], cities[order[(i + 1) % n]]);
    }
    return total;
}

int parse_cities(const char* data, int len, City* out, int cap) {
    int n = 0;
    int line_start = 0;
    bool header_skipped = false;
    for (int i = 0; i <= len; ++i) {
        const bool eol = i == len || data[i] == '\n';
        if (!eol) continue;
        if (!header_skipped) {
            header_skipped = true;
            line_start = i + 1;
            continue;
        }
        int c1 = -1, c2 = -1;
        for (int k = line_start; k < i; ++k) {
            if (data[k] == ',') {
                if (c1 < 0) c1 = k;
                else if (c2 < 0) { c2 = k; break; }
            }
        }
        if (c1 > 0 && c2 > 0 && n < cap) {
            int x = 0, y = 0, sign = 1;
            for (int k = c1 + 1; k < c2; ++k) {
                if (data[k] == '-') sign = -1;
                else if (data[k] >= '0' && data[k] <= '9') x = x * 10 + (data[k] - '0');
            }
            x *= sign;
            sign = 1;
            for (int k = c2 + 1; k < i; ++k) {
                if (data[k] == '-') sign = -1;
                else if (data[k] >= '0' && data[k] <= '9') y = y * 10 + (data[k] - '0');
            }
            y *= sign;
            out[n++] = {x, y};
        }
        line_start = i + 1;
    }
    return n;
}

void shuffle(int* order, int n, uint64_t& rng) {
    for (int i = n - 1; i > 0; --i) {
        rng ^= rng << 13;
        rng ^= rng >> 7;
        rng ^= rng << 17;
        const int j = static_cast<int>(rng % static_cast<uint64_t>(i + 1));
        const int tmp = order[i];
        order[i] = order[j];
        order[j] = tmp;
    }
}

bool two_opt_pass(const City* cities, int* order, int n) {
    bool improved = false;
    for (int i = 0; i < n - 1; ++i) {
        for (int j = i + 2; j < n; ++j) {
            const int a = order[i], b = order[i + 1];
            const int c = order[j], d = order[(j + 1) % n];
            if (a == d) continue;
            const int before = dist_sq(cities[a], cities[b]) + dist_sq(cities[c], cities[d]);
            const int after = dist_sq(cities[a], cities[c]) + dist_sq(cities[b], cities[d]);
            if (after < before) {
                int lo = i + 1, hi = j;
                while (lo < hi) {
                    const int tmp = order[lo];
                    order[lo] = order[hi];
                    order[hi] = tmp;
                    ++lo;
                    --hi;
                }
                improved = true;
            }
        }
    }
    return improved;
}
}  // namespace

WORKFLOW_NODE_WITH_CAPS(input, output, 4096, 2048) {
    const auto plan = input.node("plan-search");
    const int restarts = plan.int_("restarts_per_shard", 50000);
    const int swap_passes = plan.int_("swap_passes", 3);
    const int seed_offset = input.int_("seed_offset", 0);
    const int timeout_ms = input.int_("timeout_ms", 5000);

    char body[8192];
    const auto resp = input.artifact("cities").fetch_text(body, sizeof(body), timeout_ms);
    if (!resp.ok) {
        output.fail(30);
        return;
    }

    City cities[kMaxCities];
    const int n = parse_cities(body, resp.bytes, cities, kMaxCities);
    if (n < 3) {
        output.fail(31);
        return;
    }

    // Mix per-shard seed_offset with crypto entropy so each shard explores a
    // distinct part of the random-restart space, while two runs of the same
    // workflow remain reproducible-ish at the shard level.
    int order[kMaxCities];
    int best[kMaxCities];
    int best_length = 1 << 30;
    uint64_t rng = workflow::random_seed() ^ (static_cast<uint64_t>(seed_offset + 1) * 0x9e3779b97f4a7c15ULL);

    for (int r = 0; r < restarts; ++r) {
        for (int i = 0; i < n; ++i) order[i] = i;
        shuffle(order, n, rng);
        for (int pass = 0; pass < swap_passes; ++pass) {
            if (!two_opt_pass(cities, order, n)) break;
        }
        const int len = tour_length(cities, order, n);
        if (len < best_length) {
            best_length = len;
            for (int i = 0; i < n; ++i) best[i] = order[i];
        }
    }

    char tour_buf[384];
    int tpos = 0;
    for (int i = 0; i < n && tpos + 8 < static_cast<int>(sizeof(tour_buf)); ++i) {
        tpos += std::snprintf(tour_buf + tpos, sizeof(tour_buf) - tpos, i == 0 ? "%d" : ",%d", best[i]);
    }
    tour_buf[tpos] = '\0';

    auto json = output.object();
    json.field("tour_length", best_length);
    json.field("tour", tour_buf);
    json.field("seed_offset", seed_offset);
    json.field("restarts", restarts);
    json.field("city_count", n);
    json.done();
}
