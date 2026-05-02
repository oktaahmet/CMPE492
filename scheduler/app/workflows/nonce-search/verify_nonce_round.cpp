#include <cstdio>
#include <fstream>
#include <string>

#include "../common/sha256.hpp"
#include "../common/workflow.hpp"

namespace {
std::string message_for(workflow::Input& input) {
    const int source_round = input.int_("source_round", 0);
    if (source_round <= 0) {
        return std::string(input.string("message"));
    }

    char node_id[32];
    std::snprintf(node_id, sizeof(node_id), "verify-round-%d", source_round);
    return std::string(input.node(node_id).string("message"));
}

int bits_for(const std::string& message, long long nonce) {
    return nonce_hash::leading_zero_bits(nonce_hash::hash_nonce(message, nonce));
}

std::string extend_message(std::string message, int round, long long nonce, int bits) {
    char suffix[96];
    std::snprintf(suffix, sizeof(suffix), "|round=%d|nonce=%lld|bits=%d", round, nonce, bits);
    message += suffix;
    return message;
}

void write_round(
    std::ofstream& report,
    int round,
    int target_bits,
    int found,
    long long nonce,
    long long best_nonce,
    int best_bits,
    long long attempts) {
    report << "round " << round << "\n";
    report << "target_bits=" << target_bits << "\n";
    report << "found=" << (found != 0 ? "yes" : "no") << "\n";
    report << "nonce=" << (found != 0 ? std::to_string(nonce) : "not found") << "\n";
    report << "best_nonce=" << best_nonce << "\n";
    report << "best_bits=" << best_bits << "\n";
    report << "attempts=" << attempts << "\n\n";
}

void write_saved_round(std::ofstream& report, workflow::Input& input, const char* node_id) {
    const auto node = input.node(node_id);
    write_round(
        report,
        node.int_("round", 0),
        node.int_("target_bits", 0),
        node.int_("found", 0),
        node.number("nonce", 0),
        node.number("best_nonce", 0),
        node.int_("best_bits", 0),
        node.number("attempts", 0));
}
}

WORKFLOW_NODE(input, output) {
    const int round = input.int_("round", 1);
    const int target_bits = input.int_("target_bits", 16);
    const int write_report = input.int_("write_report", 0);
    const std::string message = message_for(input);

    int found = 0;
    long long nonce = 0;
    long long best_nonce = 0;
    int best_bits = -1;
    long long attempts = 0;

    char node_id[32];
    for (char shard = 'a'; shard <= 'c'; ++shard) {
        std::snprintf(node_id, sizeof(node_id), "round-%d-%c", round, shard);
        const auto node = input.node(node_id);
        attempts += node.number("attempts", 0);

        const long long candidate_best_nonce = node.number("best_nonce", 0);
        const int candidate_best_bits = bits_for(message, candidate_best_nonce);
        if (found == 0 && candidate_best_bits > best_bits) {
            best_nonce = candidate_best_nonce;
            best_bits = candidate_best_bits;
        }

        const long long candidate_nonce = node.number("nonce", 0);
        if (found == 0 && node.int_("found", 0) != 0 && bits_for(message, candidate_nonce) >= target_bits) {
            found = 1;
            nonce = candidate_nonce;
            best_nonce = candidate_nonce;
            best_bits = bits_for(message, candidate_nonce);
        }
    }

    const long long nonce_for_message = found != 0 ? nonce : best_nonce;
    const std::string message_for_next_round = extend_message(message, round, nonce_for_message, best_bits);

    if (write_report != 0) {
        const auto report_path = input.output_artifact("nonce-report").string("path");
        std::ofstream report{std::string(report_path)};
        if (!report) {
            output.fail(30);
            return;
        }

        report << "SHA-256 nonce search\n";
        report << "message=final-demo-block\n\n";
        write_saved_round(report, input, "verify-round-1");
        write_saved_round(report, input, "verify-round-2");
        write_round(report, round, target_bits, found, nonce, best_nonce, best_bits, attempts);
    }

    auto json = output.object();
    json.field("round", round);
    json.field("found", found);
    json.field("nonce", nonce);
    json.field("best_nonce", best_nonce);
    json.field("best_bits", best_bits);
    json.field("attempts", attempts);
    json.field("target_bits", target_bits);
    if (write_report == 0) {
        json.field("message", message_for_next_round.c_str());
    } else {
        json.field("report", "nonce_report.txt");
    }
    json.done();
}
