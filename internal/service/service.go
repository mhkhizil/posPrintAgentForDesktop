// Package service installs and runs the agent as a Windows service.
package service

import "context"

const (
	// Name is the Windows service name.
	Name = "LnkitsPrintAgent"
	// DisplayName is what the Services console shows.
	DisplayName = "Lnkits POS Print Agent"
	// Description explains the service in the Services console.
	Description = "Prints ESC/POS receipts and kitchen tickets for the Lnkits POS web app over a loopback HTTP endpoint."
)

// Runner is the long-running function a service or console start executes.
type Runner func(ctx context.Context) error
