package llm

import "time"

// Request timeouts. A provider request is bounded in two places:
//
//   - RequestTimeout bounds one non-streamed request as a whole
//     (http.Client.Timeout: dial, TLS, headers and body).
//   - ResponseHeaderTimeout bounds the wait for the response headers of a
//     streamed request (http.Transport.ResponseHeaderTimeout). The streamed body
//     itself has no deadline - a long answer may stream for minutes - and ctx
//     cancellation still stops it.
//
// The zero value of OpenAIConfig/ClaudeConfig takes the default below; NoTimeout
// switches the bound off (the config's `off`).
const (
	DefaultRequestTimeout        = 120 * time.Second
	DefaultResponseHeaderTimeout = 120 * time.Second
)

// NoTimeout disables a request timeout (OpenAIConfig/ClaudeConfig
// RequestTimeout and ResponseHeaderTimeout).
const NoTimeout time.Duration = -1

// requestTimeout resolves a configured request timeout: zero takes the default,
// NoTimeout (or any negative value) means no limit, which is what
// http.Client.Timeout uses for "off".
func requestTimeout(d time.Duration) time.Duration {
	switch {
	case d == 0:
		return DefaultRequestTimeout
	case d < 0:
		return 0
	default:
		return d
	}
}

// responseHeaderTimeout resolves a configured response header timeout the same
// way (http.Transport.ResponseHeaderTimeout 0 means no limit).
func responseHeaderTimeout(d time.Duration) time.Duration {
	switch {
	case d == 0:
		return DefaultResponseHeaderTimeout
	case d < 0:
		return 0
	default:
		return d
	}
}
