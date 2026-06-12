package gateway

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// omnihubClientHeaders are the X-OmniHub-* headers the gateway
// understands (design doc §10). They are meaningful only between the
// client and OmniHub; drivers never forward them upstream.
var omnihubClientHeaders = []string{
	"X-OmniHub-Client",
	"X-OmniHub-Client-Version",
	"X-OmniHub-Client-Platform",
	"X-OmniHub-Client-Mode",
	"X-OmniHub-Session-ID",
	"X-OmniHub-Request-ID",
	"X-OmniHub-Install-ID",
	"X-OmniHub-Capabilities",
	"X-OmniHub-Protocol",
}

// CapabilitiesHandler returns GET /v1/omnihub/capabilities — the
// capability-negotiation endpoint for OmniHub-aware clients (the
// omnihub-cli `doctor` command reads it). Auth-guarded like every
// other gateway route so it doubles as a virtual-key validity check.
func CapabilitiesHandler(serverVersion string) gin.HandlerFunc {
	payload := gin.H{
		"server_version": serverVersion,
		"protocols":      []string{"anthropic-messages", "openai-chat", "openai-responses"},
		"features":       []string{"streaming", "tools", "vision", "thinking"},
		"client_headers": omnihubClientHeaders,
	}
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, payload)
	}
}
