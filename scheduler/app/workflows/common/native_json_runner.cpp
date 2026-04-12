#include <cstdint>
#include <cstring>
#include <iostream>
#include <iterator>
#include <string>

extern "C" {
intptr_t get_input_ptr();
int get_input_capacity();
intptr_t get_output_ptr();
int get_output_len();
int run_json(int input_len);
}

int main() {
    const std::string input((std::istreambuf_iterator<char>(std::cin)), std::istreambuf_iterator<char>());

    const int input_cap = get_input_capacity();
    if (input_cap <= 0) {
        std::cerr << "invalid input capacity";
        return 10;
    }
    if (static_cast<int>(input.size()) > input_cap) {
        std::cerr << "input exceeds node buffer";
        return 11;
    }

    unsigned char* input_ptr = reinterpret_cast<unsigned char*>(static_cast<uintptr_t>(get_input_ptr()));
    if (!input_ptr) {
        std::cerr << "input pointer is null";
        return 12;
    }
    if (!input.empty()) {
        std::memcpy(input_ptr, input.data(), input.size());
    }

    const int status = run_json(static_cast<int>(input.size()));
    if (status != 0) {
        std::cerr << "run_json returned non-zero status: " << status;
        return status;
    }

    const int output_len = get_output_len();
    if (output_len < 0) {
        std::cerr << "invalid output length";
        return 13;
    }
    const char* output_ptr = reinterpret_cast<const char*>(static_cast<uintptr_t>(get_output_ptr()));
    if (!output_ptr && output_len > 0) {
        std::cerr << "output pointer is null";
        return 14;
    }

    std::cout.write(output_ptr, output_len);
    return 0;
}
