#ifndef NONCE_SEARCH_SHA256_UTIL_HPP
#define NONCE_SEARCH_SHA256_UTIL_HPP

#include <cstdint>
#include <cstdio>
#include <cstring>
#include <string_view>

namespace nonce_hash {

struct Digest {
    unsigned char bytes[32];
};

inline uint32_t rotr(uint32_t value, uint32_t bits) {
    return (value >> bits) | (value << (32 - bits));
}

class Sha256 {
  public:
    Sha256() { reset(); }

    void update(const unsigned char* data, size_t len) {
        for (size_t i = 0; i < len; ++i) {
            data_[datalen_++] = data[i];
            if (datalen_ == 64) {
                transform();
                bitlen_ += 512;
                datalen_ = 0;
            }
        }
    }

    Digest final() {
        uint32_t i = datalen_;

        if (datalen_ < 56) {
            data_[i++] = 0x80;
            while (i < 56) {
                data_[i++] = 0x00;
            }
        } else {
            data_[i++] = 0x80;
            while (i < 64) {
                data_[i++] = 0x00;
            }
            transform();
            std::memset(data_, 0, 56);
        }

        bitlen_ += static_cast<uint64_t>(datalen_) * 8;
        data_[63] = static_cast<unsigned char>(bitlen_);
        data_[62] = static_cast<unsigned char>(bitlen_ >> 8);
        data_[61] = static_cast<unsigned char>(bitlen_ >> 16);
        data_[60] = static_cast<unsigned char>(bitlen_ >> 24);
        data_[59] = static_cast<unsigned char>(bitlen_ >> 32);
        data_[58] = static_cast<unsigned char>(bitlen_ >> 40);
        data_[57] = static_cast<unsigned char>(bitlen_ >> 48);
        data_[56] = static_cast<unsigned char>(bitlen_ >> 56);
        transform();

        Digest out{};
        for (i = 0; i < 4; ++i) {
            out.bytes[i] = static_cast<unsigned char>((state_[0] >> (24 - i * 8)) & 0xff);
            out.bytes[i + 4] = static_cast<unsigned char>((state_[1] >> (24 - i * 8)) & 0xff);
            out.bytes[i + 8] = static_cast<unsigned char>((state_[2] >> (24 - i * 8)) & 0xff);
            out.bytes[i + 12] = static_cast<unsigned char>((state_[3] >> (24 - i * 8)) & 0xff);
            out.bytes[i + 16] = static_cast<unsigned char>((state_[4] >> (24 - i * 8)) & 0xff);
            out.bytes[i + 20] = static_cast<unsigned char>((state_[5] >> (24 - i * 8)) & 0xff);
            out.bytes[i + 24] = static_cast<unsigned char>((state_[6] >> (24 - i * 8)) & 0xff);
            out.bytes[i + 28] = static_cast<unsigned char>((state_[7] >> (24 - i * 8)) & 0xff);
        }
        return out;
    }

  private:
    void reset() {
        datalen_ = 0;
        bitlen_ = 0;
        state_[0] = 0x6a09e667;
        state_[1] = 0xbb67ae85;
        state_[2] = 0x3c6ef372;
        state_[3] = 0xa54ff53a;
        state_[4] = 0x510e527f;
        state_[5] = 0x9b05688c;
        state_[6] = 0x1f83d9ab;
        state_[7] = 0x5be0cd19;
    }

    void transform() {
        static constexpr uint32_t k[64] = {
            0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5,
            0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
            0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3,
            0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
            0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc,
            0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
            0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7,
            0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
            0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13,
            0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
            0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3,
            0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
            0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5,
            0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
            0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208,
            0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2,
        };

        uint32_t m[64];
        for (uint32_t i = 0, j = 0; i < 16; ++i, j += 4) {
            m[i] = (static_cast<uint32_t>(data_[j]) << 24) |
                   (static_cast<uint32_t>(data_[j + 1]) << 16) |
                   (static_cast<uint32_t>(data_[j + 2]) << 8) |
                   static_cast<uint32_t>(data_[j + 3]);
        }
        for (uint32_t i = 16; i < 64; ++i) {
            const uint32_t s0 = rotr(m[i - 15], 7) ^ rotr(m[i - 15], 18) ^ (m[i - 15] >> 3);
            const uint32_t s1 = rotr(m[i - 2], 17) ^ rotr(m[i - 2], 19) ^ (m[i - 2] >> 10);
            m[i] = m[i - 16] + s0 + m[i - 7] + s1;
        }

        uint32_t a = state_[0];
        uint32_t b = state_[1];
        uint32_t c = state_[2];
        uint32_t d = state_[3];
        uint32_t e = state_[4];
        uint32_t f = state_[5];
        uint32_t g = state_[6];
        uint32_t h = state_[7];

        for (uint32_t i = 0; i < 64; ++i) {
            const uint32_t s1 = rotr(e, 6) ^ rotr(e, 11) ^ rotr(e, 25);
            const uint32_t ch = (e & f) ^ ((~e) & g);
            const uint32_t temp1 = h + s1 + ch + k[i] + m[i];
            const uint32_t s0 = rotr(a, 2) ^ rotr(a, 13) ^ rotr(a, 22);
            const uint32_t maj = (a & b) ^ (a & c) ^ (b & c);
            const uint32_t temp2 = s0 + maj;

            h = g;
            g = f;
            f = e;
            e = d + temp1;
            d = c;
            c = b;
            b = a;
            a = temp1 + temp2;
        }

        state_[0] += a;
        state_[1] += b;
        state_[2] += c;
        state_[3] += d;
        state_[4] += e;
        state_[5] += f;
        state_[6] += g;
        state_[7] += h;
    }

    unsigned char data_[64]{};
    uint32_t datalen_ = 0;
    uint64_t bitlen_ = 0;
    uint32_t state_[8]{};
};

inline Digest hash_nonce(std::string_view message, long long nonce) {
    char nonce_text[64];
    const int nonce_len = std::snprintf(nonce_text, sizeof(nonce_text), ":%lld", nonce);

    Sha256 sha;
    sha.update(reinterpret_cast<const unsigned char*>(message.data()), message.size());
    sha.update(reinterpret_cast<const unsigned char*>(nonce_text), nonce_len > 0 ? static_cast<size_t>(nonce_len) : 0);
    return sha.final();
}

inline int leading_zero_bits(const Digest& digest) {
    int bits = 0;
    for (unsigned char byte : digest.bytes) {
        if (byte == 0) {
            bits += 8;
            continue;
        }
        for (int bit = 7; bit >= 0; --bit) {
            if ((byte & (1u << bit)) == 0) {
                ++bits;
            } else {
                return bits;
            }
        }
    }
    return bits;
}

inline long long low63(const Digest& digest) {
    uint64_t value = 0;
    for (int i = 0; i < 8; ++i) {
        value = (value << 8) | digest.bytes[i];
    }
    return static_cast<long long>(value & 0x7FFFFFFFFFFFFFFFULL);
}

}  // namespace nonce_hash

#endif
