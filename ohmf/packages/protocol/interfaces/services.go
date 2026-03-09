package interfaces

// This package defines minimal interfaces representing core platform services
// referenced in the OHMF high-level architecture. Concrete services may implement
// these interfaces; they are kept intentionally small as markers for runtime
// composition and wiring.

type AuthService interface {
    // marker: implements auth flows
    StartPhoneVerification(any) (any, error)
}

type UserService interface{}
type ConversationService interface{}
type MessageService interface{}
type RelayService interface{}
type PresenceService interface{}
type NotificationService interface{}
type MediaService interface{}
type MiniAppService interface{}
type AbuseService interface{}
