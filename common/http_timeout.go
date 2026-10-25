package common

import "os"

// Keep the deployed legacy setting, but let the upstream name take precedence.
func relayResponseHeaderTimeoutFromEnv() int {
	if _, exists := os.LookupEnv("RELAY_RESPONSE_HEADER_TIMEOUT"); exists {
		return GetEnvOrDefault("RELAY_RESPONSE_HEADER_TIMEOUT", 1800)
	}
	return GetEnvOrDefault("RESPONSE_HEADER_TIMEOUT", 1800)
}
