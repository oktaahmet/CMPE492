#ifndef WORKFLOW_RUNTIME_BROWSER_HPP
#define WORKFLOW_RUNTIME_BROWSER_HPP

#include <cstdint>
#include <cstring>

namespace runtime_browser {

enum FetchErrorCode {
    FETCH_OK = 0,
    FETCH_INVALID_ARGS = 1,
    FETCH_INVALID_MEMORY = 2,
    FETCH_INVALID_URL = 3,
    FETCH_BAD_PROTOCOL = 4,
    FETCH_REQUEST_FAILED = 5,
    FETCH_TIMEOUT = 6,
    FETCH_HTTP_STATUS = 7,
    FETCH_RESPONSE_TOO_LARGE = 8,
};

struct FetchResult {
    bool ok;
    int bytes;
    int http_status;
    int error_code;
    const char* data;
};

extern "C" {
__attribute__((import_module("env"), import_name("fetchx_text"))) int fetchx_text_import(
    int url_ptr,
    int url_len,
    int out_ptr,
    int out_cap,
    int timeout_ms);
__attribute__((import_module("env"), import_name("fetchx_last_http_status"))) int fetchx_last_http_status_import();
__attribute__((import_module("env"), import_name("fetchx_last_error_code"))) int fetchx_last_error_code_import();
__attribute__((import_module("env"), import_name("fetchx_max_response_bytes"))) int fetchx_max_response_bytes_import();
}

inline int fetch_text(const char* url, int url_len, char* out, int out_cap, int timeout_ms) {
    if (!url || url_len <= 0 || !out || out_cap <= 0) {
        return -1;
    }
    return fetchx_text_import(
        static_cast<int>(reinterpret_cast<uintptr_t>(url)),
        url_len,
        static_cast<int>(reinterpret_cast<uintptr_t>(out)),
        out_cap,
        timeout_ms);
}

inline int fetch_text(const char* url, char* out, int out_cap, int timeout_ms = 3000) {
    if (!url || !out || out_cap <= 0) {
        return -1;
    }
    const int n = fetch_text(url, static_cast<int>(std::strlen(url)), out, out_cap, timeout_ms);
    if (n >= 0) {
        const int end = n < out_cap ? n : out_cap - 1;
        out[end] = '\0';
    } else {
        out[0] = '\0';
    }
    return n;
}

inline FetchResult get(const char* url, char* out, int out_cap, int timeout_ms = 3000) {
    const int n = fetch_text(url, out, out_cap, timeout_ms);
    return {
        n >= 0,
        n >= 0 ? n : 0,
        fetchx_last_http_status_import(),
        fetchx_last_error_code_import(),
        out,
    };
}

inline int last_http_status() { return fetchx_last_http_status_import(); }

inline int last_error_code() { return fetchx_last_error_code_import(); }

inline int max_response_bytes() { return fetchx_max_response_bytes_import(); }

}  // namespace runtime_browser

#endif
