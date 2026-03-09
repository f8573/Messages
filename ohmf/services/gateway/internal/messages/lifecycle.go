package messages

// Lifecycle states for messages per OHMF spec (Section 14)
const (
    LifecycleQueued  = "QUEUED"
    LifecycleAccepted = "ACCEPTED"
    LifecycleStored  = "STORED"
    LifecyclePushed  = "PUSHED"
    LifecycleDelivered = "DELIVERED"
    LifecycleRead    = "READ"
    LifecycleFailed  = "FAILED"

    // SMS/MMS specific
    LifecyclePendingLocal   = "PENDING_LOCAL"
    LifecycleSentToModem    = "SENT_TO_MODEM"
    LifecycleSentToCarrier  = "SENT_TO_CARRIER"
    LifecycleFailedLocal    = "FAILED_LOCAL"
    LifecycleFailedCarrier  = "FAILED_CARRIER"
)

// validTransitions maps allowed lifecycle state transitions.
var validTransitions = map[string]map[string]bool{
    LifecycleQueued: {
        LifecycleAccepted: true,
        LifecycleFailed:   true,
    },
    LifecycleAccepted: {
        LifecycleStored: true,
        LifecycleFailed: true,
    },
    LifecycleStored: {
        LifecyclePushed: true,
        LifecycleFailed: true,
    },
    LifecyclePushed: {
        LifecycleDelivered: true,
        LifecycleFailed:    true,
    },
    LifecycleDelivered: {
        LifecycleRead: true,
    },

    // SMS/MMS flow
    LifecyclePendingLocal: {
        LifecycleSentToModem: true,
        LifecycleFailedLocal: true,
    },
    LifecycleSentToModem: {
        LifecycleSentToCarrier: true,
        LifecycleFailedLocal:   true,
    },
    LifecycleSentToCarrier: {
        LifecycleDelivered: true,
        LifecycleFailedCarrier: true,
    },
}

// IsValidLifecycleTransition returns true if moving from `from` to `to` is allowed.
func IsValidLifecycleTransition(from, to string) bool {
    if from == to {
        return true
    }
    if allowed, ok := validTransitions[from]; ok {
        return allowed[to]
    }
    return false
}
