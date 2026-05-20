package wire

const (
	// NotificationsMethodSend is emitted by the daemon (as an event) to
	// have the relay deliver a Web Push notification to all of the
	// account's subscribed devices.
	NotificationsMethodSend = "send"
)

// NotificationsSendEvent is the payload for notifications.send events.
type NotificationsSendEvent struct {
	Title     string `json:"title"`
	Body      string `json:"body,omitempty"`
	Tag       string `json:"tag,omitempty"`
	URL       string `json:"url,omitempty"`
	Icon      string `json:"icon,omitempty"`
	SessionID string `json:"session_id,omitempty"`
}
