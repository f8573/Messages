package types

// Content types
const (
    ContentTypeText    = "text"
    ContentTypeRich    = "rich_text"
    ContentTypeMedia   = "media"
    ContentTypeAppCard = "app_card"
    ContentTypeAppEvent = "app_event"
    ContentTypeSystem  = "system"
)

// Transport types
const (
    TransportOTT    = "OTT"
    TransportSMS    = "SMS"
    TransportMMS    = "MMS"
    TransportRelaySMS = "RELAY_SMS"
    TransportRelayMMS = "RELAY_MMS"
)

// Visibility states
const (
    VisibilityActive   = "ACTIVE"
    VisibilityEdited   = "EDITED"
    VisibilitySoftDeleted = "SOFT_DELETED"
    VisibilityRedacted = "REDACTED"
    VisibilityPurged   = "PURGED"
)

// Capability names
const (
    CapabilityOTT       = "OTT"
    CapabilityPush      = "PUSH"
    CapabilityMiniApps  = "MINI_APPS"
    CapabilitySMSHandler = "SMS_HANDLER"
    CapabilityRelayExec = "RELAY_EXECUTOR"
)

// Client modes and transport policies
const (
    ClientModeOTTOnly       = "OTT_ONLY"
    ClientModeDefaultSMS    = "DEFAULT_SMS_HANDLER"

    TransportPolicyAuto     = "AUTO"
    TransportPolicyForceOTT = "FORCE_OTT"
    TransportPolicyForceSMS = "FORCE_SMS"
    TransportPolicyForceMMS = "FORCE_MMS"
    TransportPolicyBlockCarrierRelay = "BLOCK_CARRIER_RELAY"
)

// Carrier mirroring modes
const (
    MirroringNone = "NONE"
    MirroringMetadataOnly = "METADATA_ONLY"
    MirroringFullContent = "FULL_CONTENT"
    MirroringSelective = "SELECTIVE"
)
