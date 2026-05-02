#include "../common/workflow.hpp"

WORKFLOW_NODE_WITH_CAPS(input, output, 64 * 1024, 1024) {
    const int metadata_bytes = input.node("fetch-queueing-metadata").int_("metadata_bytes", 0);
    const int metadata_word_count = input.node("fetch-queueing-metadata").int_("metadata_word_count", 0);
    const int metadata_digit_count = input.node("fetch-queueing-metadata").int_("metadata_digit_count", 0);
    const int metadata_queue_mentions = input.node("fetch-queueing-metadata").int_("metadata_queue_mentions", 0);
    const int metadata_title_mentions = input.node("fetch-queueing-metadata").int_("metadata_title_mentions", 0);
    if (metadata_bytes <= 0) {
        output.fail(2);
        return;
    }

    const int customer_floor = input.int_("customer_floor", 420);
    const int customer_span = input.int_("customer_span", 220);
    const int interarrival_floor_ms = input.int_("interarrival_floor_ms", 92);
    const int service_floor_ms = input.int_("service_floor_ms", 68);
    const int stress_floor_pct = input.int_("stress_floor_pct", 130);

    const int customers = customer_floor + (metadata_bytes % (customer_span <= 0 ? 1 : customer_span));
    const int mean_interarrival_ms = interarrival_floor_ms + (metadata_word_count % 45);
    const int mean_service_ms = service_floor_ms + ((metadata_queue_mentions * 7 + metadata_title_mentions * 3 + metadata_digit_count) % 40);
    const int stress_scale_pct = stress_floor_pct + (metadata_digit_count % 31);

    auto json = output.object();
    json.field("customers", customers);
    json.field("mean_interarrival_ms", mean_interarrival_ms);
    json.field("mean_service_ms", mean_service_ms);
    json.field("stress_scale_pct", stress_scale_pct);
    json.field("metadata_bytes", metadata_bytes);
    json.field("metadata_queue_mentions", metadata_queue_mentions);
    json.done();
}
