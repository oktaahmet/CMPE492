#ifndef WORKFLOW_RUNTIME_NODE_HPP
#define WORKFLOW_RUNTIME_NODE_HPP

#include <cstdint>

#ifdef __EMSCRIPTEN__
#include <emscripten/emscripten.h>
#define WORKFLOW_RUNTIME_PTR int
#else
#define EMSCRIPTEN_KEEPALIVE
#define WORKFLOW_RUNTIME_PTR intptr_t
#endif

#define WORKFLOW_JSON_NODE(INPUT_CAP, OUTPUT_CAP)                                                                        \
    namespace workflow_runtime_abi {                                                                                    \
    constexpr int kInputCap = (INPUT_CAP);                                                                              \
    constexpr int kOutputCap = (OUTPUT_CAP);                                                                            \
    unsigned char g_input[kInputCap];                                                                                   \
    char g_output[kOutputCap];                                                                                          \
    int g_output_len = 0;                                                                                               \
    }                                                                                                                   \
    int workflow_run_json(const char* input, int input_len, char* output, int output_cap, int& output_len);              \
    extern "C" {                                                                                                        \
    EMSCRIPTEN_KEEPALIVE WORKFLOW_RUNTIME_PTR get_input_ptr() {                                                         \
        return static_cast<WORKFLOW_RUNTIME_PTR>(reinterpret_cast<uintptr_t>(workflow_runtime_abi::g_input));            \
    }                                                                                                                   \
    EMSCRIPTEN_KEEPALIVE int get_input_capacity() { return workflow_runtime_abi::kInputCap; }                           \
    EMSCRIPTEN_KEEPALIVE WORKFLOW_RUNTIME_PTR get_output_ptr() {                                                        \
        return static_cast<WORKFLOW_RUNTIME_PTR>(reinterpret_cast<uintptr_t>(workflow_runtime_abi::g_output));           \
    }                                                                                                                   \
    EMSCRIPTEN_KEEPALIVE int get_output_len() { return workflow_runtime_abi::g_output_len; }                            \
    EMSCRIPTEN_KEEPALIVE int run_json(int input_len) {                                                                  \
        if (input_len < 0 || input_len > workflow_runtime_abi::kInputCap) {                                              \
            return 1;                                                                                                   \
        }                                                                                                               \
        return workflow_run_json(                                                                                       \
            reinterpret_cast<const char*>(workflow_runtime_abi::g_input),                                                \
            input_len,                                                                                                  \
            workflow_runtime_abi::g_output,                                                                             \
            workflow_runtime_abi::kOutputCap,                                                                           \
            workflow_runtime_abi::g_output_len);                                                                        \
    }                                                                                                                   \
    }

#endif
