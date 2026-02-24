#include <emscripten/emscripten.h>

extern "C" {
EMSCRIPTEN_KEEPALIVE int run(int start, int end) {
    if (end < start) {
        return 0;
    }
    return (end - start) + 1;
}
}
