package wire

const (
	PortsMethodList  = "list"
	PortsMethodFetch = "fetch"
)

// PortsEntry mirrors internal/portsapi.PortInfo.
type PortsEntry struct {
	Port    uint16 `json:"port"`
	Address string `json:"address"`
	Family  string `json:"family"`
	PID     int    `json:"pid,omitempty"`
	Command string `json:"command,omitempty"`
}

type PortsListResponse struct {
	Entries []PortsEntry `json:"entries"`
}

// PortsHeader is one (name, value) pair on the wire — JSON object map order
// is unspecified so we use a flat list to preserve insertion order.
type PortsHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// PortsFetchRequest forwards an HTTP request to a localhost port. Body bytes
// are base64-encoded; an empty Body field means no request body.
type PortsFetchRequest struct {
	Port         uint16        `json:"port"`
	Family       string        `json:"family,omitempty"`
	Method       string        `json:"method"`
	Path         string        `json:"path"`
	Headers      []PortsHeader `json:"headers,omitempty"`
	Body         string        `json:"body,omitempty"`
	MaxBodyBytes int64         `json:"max_body_bytes,omitempty"`
}

// PortsFetchResponse is what the daemon returns. Body is base64.
type PortsFetchResponse struct {
	Status     int           `json:"status"`
	Headers    []PortsHeader `json:"headers"`
	Body       string        `json:"body,omitempty"`
	Truncated  bool          `json:"truncated,omitempty"`
	UpstreamMS int64         `json:"upstream_ms,omitempty"`
}
