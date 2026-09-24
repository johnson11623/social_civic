package events

// Topics. Each event type is published to the topic of the same name, keyed
// by the aggregate id so events for one entity stay ordered.
const (
	TopicUserRegistered = "user.registered"
)

// AllTopics lists every topic the platform publishes to, for provisioning.
var AllTopics = []string{
	TopicUserRegistered,
}
