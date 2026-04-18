#ifndef WORKFLOW_RUNTIME_ARTIFACTS_HPP
#define WORKFLOW_RUNTIME_ARTIFACTS_HPP

#include <cstdio>
#include <cstring>

#include "runtime_browser.hpp"
#include "runtime_json.hpp"

namespace runtime_artifacts {

struct TextArtifact {
    bool ok;
    int bytes;
    int http_status;
    int error_code;
    const char* data;
};

using BytesArtifact = TextArtifact;

inline int copy_string(const char* src, int len, char* out, int cap) {
    if (!src || len <= 0 || !out || cap <= 0) {
        return 0;
    }
    const int n = len < cap - 1 ? len : cap - 1;
    std::memcpy(out, src, static_cast<size_t>(n));
    out[n] = '\0';
    return n;
}

inline TextArtifact fetch_text_by_key(
    const char* input,
    int input_len,
    const char* artifact_key,
    char* out,
    int out_cap,
    int timeout_ms,
    char* url_buf,
    int url_cap) {
    const char* url_ptr = nullptr;
    const int url_len = runtime_json::extract_named_string(input, input_len, artifact_key, &url_ptr);
    if (copy_string(url_ptr, url_len, url_buf, url_cap) <= 0) {
        return {false, 0, 0, runtime_browser::FETCH_INVALID_URL, out};
    }

    const runtime_browser::FetchResult fetched = runtime_browser::get(url_buf, out, out_cap, timeout_ms);
    return {fetched.ok, fetched.bytes, fetched.http_status, fetched.error_code, fetched.data};
}

inline TextArtifact fetch_text(
    const char* input,
    int input_len,
    const char* artifact_id,
    char* out,
    int out_cap,
    int timeout_ms,
    char* url_buf,
    int url_cap) {
    const int key_cap = 128;
    char key[key_cap];
    const int written = std::snprintf(key, key_cap, "%s_url", artifact_id ? artifact_id : "");
    if (written > 0 && written < key_cap) {
        TextArtifact by_specific_key = fetch_text_by_key(input, input_len, key, out, out_cap, timeout_ms, url_buf, url_cap);
        if (by_specific_key.ok || by_specific_key.error_code != runtime_browser::FETCH_INVALID_URL) {
            return by_specific_key;
        }
    }

    return fetch_text_by_key(input, input_len, "url", out, out_cap, timeout_ms, url_buf, url_cap);
}

inline BytesArtifact fetch_bytes_by_key(
    const char* input,
    int input_len,
    const char* artifact_key,
    unsigned char* out,
    int out_cap,
    int timeout_ms,
    char* url_buf,
    int url_cap) {
    const char* url_ptr = nullptr;
    const int url_len = runtime_json::extract_named_string(input, input_len, artifact_key, &url_ptr);
    if (copy_string(url_ptr, url_len, url_buf, url_cap) <= 0) {
        return {false, 0, 0, runtime_browser::FETCH_INVALID_URL, reinterpret_cast<const char*>(out)};
    }

    const runtime_browser::FetchResult fetched = runtime_browser::get_bytes(url_buf, out, out_cap, timeout_ms);
    return {fetched.ok, fetched.bytes, fetched.http_status, fetched.error_code, fetched.data};
}

inline BytesArtifact fetch_bytes(
    const char* input,
    int input_len,
    const char* artifact_id,
    unsigned char* out,
    int out_cap,
    int timeout_ms,
    char* url_buf,
    int url_cap) {
    const int key_cap = 128;
    char key[key_cap];
    const int written = std::snprintf(key, key_cap, "%s_url", artifact_id ? artifact_id : "");
    if (written > 0 && written < key_cap) {
        BytesArtifact by_specific_key = fetch_bytes_by_key(input, input_len, key, out, out_cap, timeout_ms, url_buf, url_cap);
        if (by_specific_key.ok || by_specific_key.error_code != runtime_browser::FETCH_INVALID_URL) {
            return by_specific_key;
        }
    }

    return fetch_bytes_by_key(input, input_len, "url", out, out_cap, timeout_ms, url_buf, url_cap);
}

inline int write_fetch_error(char* out, int out_cap, const TextArtifact& artifact) {
    if (!out || out_cap <= 0) {
        return 0;
    }
    const int n = std::snprintf(
        out,
        out_cap,
        "{\"ok\":false,\"fetch_error_code\":%d,\"http_status\":%d}",
        artifact.error_code,
        artifact.http_status);
    return n < 0 || n >= out_cap ? 0 : n;
}

}  // namespace runtime_artifacts

#endif
