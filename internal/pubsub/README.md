# Broker delivery contract

Publishing never performs I/O or waits for a subscriber to receive. A short mutex
serializes in-memory sends with cancellation and channel closure. Shutdown closes
subscriptions and joins their context/shutdown waiters, including subscriptions
whose contexts were never cancelled.

`NewBrokerWithOptions(channelBufferSize, maxEvents)` uses a positive channel
capacity bounded by `maxEvents`. Nonpositive capacities become one; a nonpositive
`maxEvents` uses 1000. The default channel capacity remains 64.

Delivery preserves publication order while the channel has capacity. When a slow
consumer fills it, the oldest queued delivery is replaced by the newest event.
The newest event has `ReconcileRequired=true`; repeated saturation preserves that
signal even if an earlier marked event is removed. `OverflowCount` counts dropped
queued deliveries across subscribers, not application operations.

This is a bounded notification stream, not an unbounded durable event log. A
consumer receiving `ReconcileRequired` must reload its authoritative state before
applying further transitions: message/session storage, pending permissions, or the
current agent/workflow state as appropriate. This applies to creation, deletion,
and completion too; replaying an incomplete notification tail is insufficient.
Adapters forwarding notifications must call `PublishEvent` to preserve the flag.
Authoritative state must be published before its notification. Disposable log
consumers can display an explicit gap rather than inventing missing history.

No publisher blocks, no extra application actions are replayed for recovery, and
capacity never grows with the number of tokens or events.
