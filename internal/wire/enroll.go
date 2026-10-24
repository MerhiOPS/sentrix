package wire

// EnrollRequest exchanges a single-use enrollment token for a long-lived
// per-host credential. The token secret is sent only after the agent has
// verified the server's key against the pin carried in the token.
type EnrollRequest struct {
	Token string   `json:"token"`
	Host  HostInfo `json:"host"`
}

// HostInfo describes the machine at enrollment. It is informational: the
// server never trusts it for identity — tenant and host come from the token
// and, afterwards, from the credential.
type HostInfo struct {
	Hostname     string `json:"hostname"`
	OS           string `json:"os"`
	Platform     string `json:"platform"`
	AgentVersion string `json:"agent_version"`
	IntervalMS   int64  `json:"interval_ms"`
}

// EnrollResponse carries the credential. The agent stores it (0600) and
// sends it as "Authorization: Bearer <credential>" on every batch.
type EnrollResponse struct {
	Credential string `json:"credential"`
	HostID     string `json:"host_id"`
}

// AuthScheme is the Authorization header scheme for agent credentials.
const AuthScheme = "Bearer"
