#ifndef WORKFLOW_AUTHORING_HPP
#define WORKFLOW_AUTHORING_HPP

#include <cstdint>
#include <cstdio>
#include <cstring>
#include <string>
#include <string_view>
#include <vector>

#include "runtime_browser.hpp"
#include "runtime_json.hpp"
#include "runtime_node.hpp"
#include "runtime_random.hpp"

namespace workflow {

struct Status {
    int code = 0;

    void fail(int next_code) {
        if (code == 0) {
            code = next_code;
        }
    }
};

namespace detail {

inline bool key_at(const char* data, int len, int i, const char* key) {
    const int key_len = static_cast<int>(std::strlen(key));
    if (i < 0 || i + key_len + 2 > len || data[i] != '"') {
        return false;
    }
    for (int j = 0; j < key_len; ++j) {
        if (data[i + 1 + j] != key[j]) {
            return false;
        }
    }
    return data[i + 1 + key_len] == '"';
}

inline int matching_close(const char* data, int len, int open_pos, char open_char, char close_char) {
    int depth = 0;
    bool in_string = false;
    bool escaped = false;
    for (int i = open_pos; i < len; ++i) {
        const char c = data[i];
        if (in_string) {
            if (escaped) {
                escaped = false;
            } else if (c == '\\') {
                escaped = true;
            } else if (c == '"') {
                in_string = false;
            }
            continue;
        }
        if (c == '"') {
            in_string = true;
            continue;
        }
        if (c == open_char) {
            ++depth;
        } else if (c == close_char) {
            --depth;
            if (depth == 0) {
                return i;
            }
        }
    }
    return -1;
}

inline std::string_view object_field(std::string_view object, const char* key) {
    const char* data = object.data();
    const int len = static_cast<int>(object.size());
    for (int i = 0; i < len; ++i) {
        if (!key_at(data, len, i, key)) {
            continue;
        }
        int k = i + static_cast<int>(std::strlen(key)) + 2;
        while (k < len && data[k] != ':') {
            ++k;
        }
        if (k >= len) {
            continue;
        }
        ++k;
        while (k < len && (data[k] == ' ' || data[k] == '\n' || data[k] == '\r' || data[k] == '\t')) {
            ++k;
        }
        if (k >= len || data[k] != '{') {
            continue;
        }
        const int end = matching_close(data, len, k, '{', '}');
        if (end <= k) {
            continue;
        }
        return std::string_view(data + k, static_cast<size_t>(end - k + 1));
    }
    return {};
}

inline std::string_view array_field(std::string_view object, const char* key) {
    const char* data = object.data();
    const int len = static_cast<int>(object.size());
    for (int i = 0; i < len; ++i) {
        if (!key_at(data, len, i, key)) {
            continue;
        }
        int k = i + static_cast<int>(std::strlen(key)) + 2;
        while (k < len && data[k] != ':') {
            ++k;
        }
        if (k >= len) {
            continue;
        }
        ++k;
        while (k < len && (data[k] == ' ' || data[k] == '\n' || data[k] == '\r' || data[k] == '\t')) {
            ++k;
        }
        if (k >= len || data[k] != '[') {
            continue;
        }
        const int end = matching_close(data, len, k, '[', ']');
        if (end <= k) {
            continue;
        }
        return std::string_view(data + k, static_cast<size_t>(end - k + 1));
    }
    return {};
}

inline std::string_view string_field(std::string_view object, const char* key) {
    const char* ptr = nullptr;
    const int len = runtime_json::extract_named_string(
        object.data(),
        static_cast<int>(object.size()),
        key,
        &ptr);
    if (len <= 0) {
        return {};
    }
    return std::string_view(ptr, static_cast<size_t>(len));
}

inline bool number_after_colon(const char* data, int len, int& k, long long& out) {
    while (k < len && data[k] != ':') {
        ++k;
    }
    if (k >= len) {
        return false;
    }
    ++k;
    while (k < len && (data[k] == ' ' || data[k] == '\n' || data[k] == '\r' || data[k] == '\t')) {
        ++k;
    }
    int sign = 1;
    if (k < len && data[k] == '-') {
        sign = -1;
        ++k;
    }
    if (k >= len || data[k] < '0' || data[k] > '9') {
        return false;
    }
    long long value = 0;
    while (k < len && data[k] >= '0' && data[k] <= '9') {
        value = value * 10 + static_cast<long long>(data[k] - '0');
        ++k;
    }
    out = value * sign;
    return true;
}

inline bool first_number_field(std::string_view object, const char* key, long long& out) {
    const char* data = object.data();
    const int len = static_cast<int>(object.size());
    for (int i = 0; i < len; ++i) {
        if (!key_at(data, len, i, key)) {
            continue;
        }
        int k = i + static_cast<int>(std::strlen(key)) + 2;
        if (number_after_colon(data, len, k, out)) {
            return true;
        }
    }
    return false;
}

inline void collect_number_fields(std::string_view object, const char* key, std::vector<long long>& out) {
    const char* data = object.data();
    const int len = static_cast<int>(object.size());
    for (int i = 0; i < len; ++i) {
        if (!key_at(data, len, i, key)) {
            continue;
        }
        int k = i + static_cast<int>(std::strlen(key)) + 2;
        long long value = 0;
        if (number_after_colon(data, len, k, value)) {
            out.push_back(value);
        }
    }
}

inline void parse_number_array(std::string_view array, std::vector<int>& out) {
    const char* data = array.data();
    const int len = static_cast<int>(array.size());
    int value = 0;
    bool in_number = false;
    for (int i = 0; i < len; ++i) {
        const char c = data[i];
        if (c >= '0' && c <= '9') {
            value = value * 10 + static_cast<int>(c - '0');
            in_number = true;
            continue;
        }
        if (in_number) {
            out.push_back(value);
            value = 0;
            in_number = false;
        }
    }
    if (in_number) {
        out.push_back(value);
    }
}

inline std::string_view first_arg(std::string_view root) {
    const std::string_view args = array_field(root, "args");
    const char* data = args.data();
    const int len = static_cast<int>(args.size());
    for (int i = 0; i < len; ++i) {
        if (data[i] != '{') {
            continue;
        }
        const int end = matching_close(data, len, i, '{', '}');
        if (end <= i) {
            continue;
        }
        return std::string_view(data + i, static_cast<size_t>(end - i + 1));
    }
    return {};
}

}  // namespace detail

class Object {
  public:
    Object() = default;
    Object(std::string_view data, Status* status = nullptr) : data_(data), status_(status) {}

    bool ok() const { return !data_.empty(); }

    std::string_view view() const { return data_; }

    Object object(const char* key) const {
        const std::string_view value = detail::object_field(data_, key);
        if (value.empty() && status_) {
            status_->fail(2);
        }
        return Object(value, status_);
    }

    std::string_view string(const char* key) const {
        const std::string_view value = detail::string_field(data_, key);
        if (value.empty() && status_) {
            status_->fail(3);
        }
        return value;
    }

    char char_(const char* key) const {
        const std::string_view value = string(key);
        if (value.size() != 1) {
            if (status_) {
                status_->fail(4);
            }
            return '\0';
        }
        return value[0];
    }

    int int_(const char* key, int fallback = 0) const {
        return static_cast<int>(number(key, fallback));
    }

    long long number(const char* key, long long fallback = 0) const {
        long long value = fallback;
        if (!detail::first_number_field(data_, key, value) && status_) {
            status_->fail(5);
        }
        return value;
    }

    std::vector<long long> numbers(const char* key) const {
        std::vector<long long> out;
        detail::collect_number_fields(data_, key, out);
        if (out.empty() && status_) {
            status_->fail(6);
        }
        return out;
    }

    std::vector<int> number_array(const char* key) const {
        std::vector<int> out;
        detail::parse_number_array(detail::array_field(data_, key), out);
        return out;
    }

    runtime_browser::FetchResult fetch_text(char* out, int out_cap, int timeout_ms = 3000) const {
        const std::string_view url = string("url");
        if (url.empty() || url.size() >= 2048) {
            if (status_) {
                status_->fail(7);
            }
            return {false, 0, 0, runtime_browser::FETCH_INVALID_URL, out};
        }

        char url_buf[2048];
        std::memcpy(url_buf, url.data(), url.size());
        url_buf[url.size()] = '\0';

        const runtime_browser::FetchResult fetched = runtime_browser::get(url_buf, out, out_cap, timeout_ms);
        if (!fetched.ok && status_) {
            status_->fail(8);
        }
        return fetched;
    }

  private:
    std::string_view data_;
    Status* status_ = nullptr;
};

using Json = Object;

inline runtime_browser::FetchResult fetch_text(std::string_view url, char* out, int out_cap, int timeout_ms = 3000) {
    static char url_buf[2048];
    if (url.empty() || url.size() >= sizeof(url_buf)) {
        return {false, 0, 0, runtime_browser::FETCH_INVALID_URL, out};
    }
    std::memcpy(url_buf, url.data(), url.size());
    url_buf[url.size()] = '\0';
    return runtime_browser::get(url_buf, out, out_cap, timeout_ms);
}

inline Json fetch_json(std::string_view url, int timeout_ms = 3000) {
    static char body[64 * 1024];
    const runtime_browser::FetchResult fetched = fetch_text(url, body, static_cast<int>(sizeof(body)), timeout_ms);
    if (!fetched.ok) {
        return {};
    }
    return Json(std::string_view(body, static_cast<size_t>(fetched.bytes)));
}

inline uint32_t random_u32() {
    uint32_t value = 0;
    if (!runtime_random::fill_bytes(&value, static_cast<int>(sizeof(value)))) {
        return 0;
    }
    return value;
}

inline uint64_t random_seed() {
    return runtime_random::seed_u64();
}

inline long long random_between(long long min_value, long long max_value) {
    if (max_value <= min_value) {
        return min_value;
    }
    const uint32_t span = static_cast<uint32_t>(max_value - min_value + 1);
    return min_value + static_cast<long long>(random_u32() % span);
}

class Input {
  public:
    Input(const char* data, int len, Status& status)
        : root_(data, static_cast<size_t>(len)), status_(status) {}

    std::string_view string(const char* key) const { return arg().string(key); }

    char char_(const char* key) const { return arg().char_(key); }

    int int_(const char* key, int fallback = 0) const { return arg().int_(key, fallback); }

    long long number(const char* key, long long fallback = 0) const { return arg().number(key, fallback); }

    Object node(const char* node_id) const {
        const std::string_view inputs = detail::object_field(root_, "inputs");
        const std::string_view node = detail::object_field(inputs, node_id);
        if (node.empty()) {
            status_.fail(9);
        }
        return Object(node, &status_);
    }

    Object optional_node(const char* node_id) const {
        const std::string_view inputs = detail::object_field(root_, "inputs");
        return Object(detail::object_field(inputs, node_id), nullptr);
    }

    Object artifact(const char* artifact_id) const {
        const std::string_view artifacts = detail::object_field(root_, "artifacts");
        const std::string_view top_level = detail::object_field(artifacts, artifact_id);
        if (!top_level.empty()) {
            return Object(top_level, &status_);
        }

        const std::string_view anywhere = detail::object_field(root_, artifact_id);
        if (anywhere.empty()) {
            status_.fail(10);
        }
        return Object(anywhere, &status_);
    }

    Object output_artifact(const char* artifact_id) const {
        const std::string_view artifacts = detail::object_field(root_, "output_artifacts");
        const std::string_view artifact = detail::object_field(artifacts, artifact_id);
        if (!artifact.empty()) {
            return Object(artifact, &status_);
        }
        return this->artifact(artifact_id);
    }

  private:
    Object arg() const {
        const std::string_view value = detail::first_arg(root_);
        if (value.empty()) {
            status_.fail(11);
        }
        return Object(value, &status_);
    }

    std::string_view root_;
    Status& status_;
};

class Output {
  public:
    Output(char* data, int cap, int& len, Status& status) : data_(data), cap_(cap), len_(len), status_(status) {
        len_ = 0;
    }

    void fail(int code) { status_.fail(code); }

    void number(long long value) {
        const int written = std::snprintf(data_, cap_, "%lld", value);
        if (written < 0 || written >= cap_) {
            status_.fail(20);
            return;
        }
        len_ = written;
    }

    void null() {
        const int written = std::snprintf(data_, cap_, "null");
        if (written != 4) {
            status_.fail(21);
            return;
        }
        len_ = written;
    }

    void text(std::string_view value) {
        if (value.size() >= static_cast<size_t>(cap_)) {
            status_.fail(22);
            return;
        }
        std::memcpy(data_, value.data(), value.size());
        len_ = static_cast<int>(value.size());
        if (len_ < cap_) {
            data_[len_] = '\0';
        }
    }

    template <typename T>
    void array(const std::vector<T>& values) {
        int pos = 0;
        if (cap_ < 2) {
            status_.fail(23);
            return;
        }
        data_[pos++] = '[';
        bool first = true;
        for (const T value : values) {
            const int written =
                std::snprintf(data_ + pos, cap_ - pos, first ? "%lld" : ",%lld", static_cast<long long>(value));
            if (written <= 0 || written >= cap_ - pos) {
                status_.fail(24);
                return;
            }
            pos += written;
            first = false;
        }
        if (pos + 2 > cap_) {
            status_.fail(25);
            return;
        }
        data_[pos++] = ']';
        data_[pos] = '\0';
        len_ = pos;
    }

    class ObjectWriter {
      public:
        ObjectWriter(char* data, int cap, int& len, Status& status)
            : json_(data, cap), len_(len), status_(status) {
            json_.begin_object();
        }

        void field(const char* key, bool value) { json_.field(key, value); }
        void field(const char* key, int value) { json_.field(key, value); }
        void field(const char* key, long long value) { json_.field(key, value); }
        void field(const char* key, const char* value) { json_.field(key, value); }

        void done() {
            len_ = json_.end_object();
            if (!json_.ok()) {
                status_.fail(26);
            }
        }

      private:
        runtime_json::JsonWriter json_;
        int& len_;
        Status& status_;
    };

    ObjectWriter object() { return ObjectWriter(data_, cap_, len_, status_); }

    int status() const { return status_.code; }

  private:
    char* data_;
    int cap_;
    int& len_;
    Status& status_;
};

}  // namespace workflow

#define WORKFLOW_NODE_WITH_CAPS(INPUT_NAME, OUTPUT_NAME, INPUT_CAP, OUTPUT_CAP)                                      \
    WORKFLOW_JSON_NODE(INPUT_CAP, OUTPUT_CAP)                                                                       \
    void workflow_node_main(workflow::Input& INPUT_NAME, workflow::Output& OUTPUT_NAME);                            \
    int workflow_run_json(                                                                                          \
        const char* raw_input,                                                                                      \
        int raw_input_len,                                                                                          \
        char* raw_output,                                                                                           \
        int raw_output_cap,                                                                                         \
        int& raw_output_len) {                                                                                      \
        workflow::Status workflow_status;                                                                           \
        workflow::Input INPUT_NAME(raw_input, raw_input_len, workflow_status);                                       \
        workflow::Output OUTPUT_NAME(raw_output, raw_output_cap, raw_output_len, workflow_status);                   \
        workflow_node_main(INPUT_NAME, OUTPUT_NAME);                                                                \
        return OUTPUT_NAME.status();                                                                                \
    }                                                                                                               \
    void workflow_node_main(workflow::Input& INPUT_NAME, workflow::Output& OUTPUT_NAME)

#define WORKFLOW_NODE(INPUT_NAME, OUTPUT_NAME) WORKFLOW_NODE_WITH_CAPS(INPUT_NAME, OUTPUT_NAME, 4096, 4096)

#endif
