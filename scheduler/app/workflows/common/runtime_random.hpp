#ifndef WORKFLOW_RUNTIME_RANDOM_HPP
#define WORKFLOW_RUNTIME_RANDOM_HPP

#include <cstdint>
#ifndef __EMSCRIPTEN__
#include <random>
#endif

namespace runtime_random {

#ifdef __EMSCRIPTEN__
extern "C" {
__attribute__((import_module("wasi_snapshot_preview1"), import_name("random_get"))) int wasi_random_get_import(
    int buf_ptr,
    int buf_len);
}

inline bool fill_bytes(void* out, int len) {
    if (!out || len <= 0) {
        return false;
    }
    return wasi_random_get_import(static_cast<int>(reinterpret_cast<uintptr_t>(out)), len) == 0;
}
#else
inline bool fill_bytes(void* out, int len) {
    if (!out || len <= 0) {
        return false;
    }
    auto* bytes = static_cast<unsigned char*>(out);
    std::random_device rd;
    for (int i = 0; i < len; ++i) {
        bytes[i] = static_cast<unsigned char>(rd());
    }
    return true;
}
#endif

inline uint64_t seed_u64() {
    uint64_t seed = 0;
    if (!fill_bytes(&seed, static_cast<int>(sizeof(seed))) || seed == 0) {
        seed = 0x9e3779b97f4a7c15ULL;
    }
    return seed;
}

}  // namespace runtime_random

#endif
