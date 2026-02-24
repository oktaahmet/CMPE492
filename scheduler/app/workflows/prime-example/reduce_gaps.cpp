#include <emscripten/emscripten.h>

extern "C" {
EMSCRIPTEN_KEEPALIVE int run(int a, int b) {
    return a + b;
}
}
