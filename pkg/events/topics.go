package events

// Topics. Each event type is published to the topic of the same name, keyed
// by the aggregate id so events for one entity stay ordered.
const (
	TopicUserRegistered     = "user.registered"
	TopicConsentWithdrawn   = "consent.withdrawn"
	TopicErasureRequested   = "erasure.requested"
	TopicUserErased         = "user.erased"
	TopicChannelCreated     = "channel.created"
	TopicPostCreated        = "post.created"
	TopicInteraction        = "interaction.recorded"
	TopicModeratorAppointed = "moderator.appointed"
)

// AllTopics lists every topic the platform publishes to, for provisioning.
var AllTopics = []string{
	TopicUserRegistered,
	TopicConsentWithdrawn,
	TopicErasureRequested,
	TopicUserErased,
	TopicChannelCreated,
	TopicPostCreated,
	TopicInteraction,
	TopicModeratorAppointed,
}
