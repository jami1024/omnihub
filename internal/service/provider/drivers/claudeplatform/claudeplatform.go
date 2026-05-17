// Package claudeplatform implements the OmniHub provider.Driver
// contract for Anthropic's "Claude on AWS Marketplace" (a.k.a. Claude
// Platform on AWS) endpoint.
//
// The wire format is the standard Anthropic Messages API; the only
// differences from the direct API are:
//
//   - URL: https://aws-external-anthropic.{aws_region}.api.aws/v1/messages
//   - Required header: anthropic-workspace-id
//   - Authentication: AWS-issued API key via x-api-key, or AWS SigV4
//     against service name "aws-external-anthropic"
//
// This MVP driver supports the **API key** authentication path. SigV4
// support lands together with the Bedrock driver, which already
// depends on the AWS SDK.
//
// Because the request body, response shape, and streaming protocol are
// identical to direct Anthropic, the driver composes the anthropic
// driver: it embeds *anthropic.Driver to inherit ParseResponse,
// DecodeStream, and Capabilities, and overrides only Name() and
// BuildRequest().
package claudeplatform

import (
	"github.com/jami1024/omnihub/internal/service/provider"
	"github.com/jami1024/omnihub/internal/service/provider/drivers/anthropic"
)

const (
	// DriverName is the string used to register and look up this driver.
	DriverName = "claude-platform"

	// AWSServiceName is the SigV4 service identifier (used later when
	// SigV4 mode is added). Documented here so the constant lives next
	// to the driver it describes.
	AWSServiceName = "aws-external-anthropic"

	// urlTemplate is the regional endpoint pattern. Filled in with the
	// account's aws_region credential.
	urlTemplate = "https://aws-external-anthropic.%s.api.aws"

	// messagesPath is the path appended to the regional base URL.
	messagesPath = "/v1/messages"
)

// Driver implements provider.Driver for Claude Platform on AWS.
type Driver struct {
	// Embed the Anthropic driver to reuse ParseResponse, DecodeStream,
	// and Capabilities verbatim. Name() and BuildRequest() are
	// overridden below.
	*anthropic.Driver
}

// New returns a new Claude Platform driver.
func New() *Driver {
	return &Driver{Driver: anthropic.New()}
}

// Name returns the driver name used for registration.
func (d *Driver) Name() string { return DriverName }

// Capabilities re-exports the embedded driver's capabilities. Explicit
// override exists only so future divergence stays a one-line change.
func (d *Driver) Capabilities() provider.Capabilities {
	return d.Driver.Capabilities()
}
