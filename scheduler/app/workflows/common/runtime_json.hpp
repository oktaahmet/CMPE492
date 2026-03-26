#ifndef WORKFLOW_RUNTIME_JSON_HPP
#define WORKFLOW_RUNTIME_JSON_HPP

#include <cctype>
#include <cstring>

namespace runtime_json {

inline int extract_ints(const char* data, int len, long long* out, int max_out) {
    int found = 0;
    int i = 0;
    while (i < len && found < max_out) {
        if (!(data[i] == '-' || std::isdigit(static_cast<unsigned char>(data[i])))) {
            ++i;
            continue;
        }
        int sign = 1;
        if (data[i] == '-') {
            sign = -1;
            ++i;
        }
        if (i >= len || !std::isdigit(static_cast<unsigned char>(data[i]))) {
            continue;
        }
        long long value = 0;
        while (i < len && std::isdigit(static_cast<unsigned char>(data[i]))) {
            value = value * 10 + static_cast<long long>(data[i] - '0');
            ++i;
        }
        out[found++] = value * sign;
    }
    return found;
}

inline long long extract_named_int(const char* data, int len, const char* key, long long fallback) {
    const int key_len = static_cast<int>(std::strlen(key));
    for (int i = 0; i + key_len + 2 < len; ++i) {
        if (data[i] != '"') {
            continue;
        }
        bool match = true;
        for (int j = 0; j < key_len; ++j) {
            if (data[i + 1 + j] != key[j]) {
                match = false;
                break;
            }
        }
        if (!match || data[i + 1 + key_len] != '"') {
            continue;
        }
        int k = i + 1 + key_len + 1;
        while (k < len && data[k] != ':') {
            ++k;
        }
        if (k >= len) {
            continue;
        }
        ++k;
        while (k < len && std::isspace(static_cast<unsigned char>(data[k]))) {
            ++k;
        }
        int sign = 1;
        if (k < len && data[k] == '-') {
            sign = -1;
            ++k;
        }
        if (k >= len || !std::isdigit(static_cast<unsigned char>(data[k]))) {
            continue;
        }
        long long value = 0;
        while (k < len && std::isdigit(static_cast<unsigned char>(data[k]))) {
            value = value * 10 + static_cast<long long>(data[k] - '0');
            ++k;
        }
        return value * sign;
    }
    return fallback;
}

inline int extract_first_output_string(const char* data, int len, const char** out_ptr) {
    const char* needle = "\"output\":\"";
    const int needle_len = 10;
    for (int i = 0; i + needle_len < len; ++i) {
        bool match = true;
        for (int j = 0; j < needle_len; ++j) {
            if (data[i + j] != needle[j]) {
                match = false;
                break;
            }
        }
        if (!match) {
            continue;
        }
        const char* start = data + i + needle_len;
        int n = 0;
        while (i + needle_len + n < len) {
            const char c = start[n];
            if (c == '"' || c == '\\') {
                break;
            }
            ++n;
        }
        *out_ptr = start;
        return n;
    }
    *out_ptr = nullptr;
    return 0;
}

inline int extract_input_output_string(const char* data, int len, const char* input_key, const char** out_ptr) {
    const int key_len = static_cast<int>(std::strlen(input_key));
    const char* output_needle = "\"output\":\"";
    const int output_len = 10;

    for (int i = 0; i + key_len + output_len + 6 < len; ++i) {
        if (data[i] != '"') {
            continue;
        }
        bool match = true;
        for (int j = 0; j < key_len; ++j) {
            if (data[i + 1 + j] != input_key[j]) {
                match = false;
                break;
            }
        }
        if (!match || data[i + 1 + key_len] != '"') {
            continue;
        }

        int k = i + key_len + 2;
        while (k + output_len < len) {
            if (data[k] == '"' && data[k + 1] == 'o') {
                bool output_match = true;
                for (int j = 0; j < output_len; ++j) {
                    if (data[k + j] != output_needle[j]) {
                        output_match = false;
                        break;
                    }
                }
                if (output_match) {
                    const char* start = data + k + output_len;
                    int n = 0;
                    while (k + output_len + n < len) {
                        const char c = start[n];
                        if (c == '"' || c == '\\') {
                            break;
                        }
                        ++n;
                    }
                    *out_ptr = start;
                    return n;
                }
            }
            if (data[k] == '}') {
                break;
            }
            ++k;
        }
    }

    *out_ptr = nullptr;
    return 0;
}

inline int extract_tagged_int(const char* data, int len, const char* prefix, int fallback) {
    const int prefix_len = static_cast<int>(std::strlen(prefix));
    if (prefix_len <= 0 || len <= prefix_len) {
        return fallback;
    }
    for (int i = 0; i + prefix_len < len; ++i) {
        bool match = true;
        for (int j = 0; j < prefix_len; ++j) {
            if (data[i + j] != prefix[j]) {
                match = false;
                break;
            }
        }
        if (!match) {
            continue;
        }
        int k = i + prefix_len;
        int sign = 1;
        if (k < len && data[k] == '-') {
            sign = -1;
            ++k;
        }
        if (k >= len || !std::isdigit(static_cast<unsigned char>(data[k]))) {
            continue;
        }
        int value = 0;
        while (k < len && std::isdigit(static_cast<unsigned char>(data[k]))) {
            value = value * 10 + static_cast<int>(data[k] - '0');
            ++k;
        }
        return value * sign;
    }
    return fallback;
}

}  // namespace runtime_json

#endif
