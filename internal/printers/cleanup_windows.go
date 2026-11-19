//go:build windows

package printers

// IsElevated reports whether the agent can administer print queues.
func IsElevated() bool { return processElevated() }

// Cleanup removes print queues whose printer is no longer attached. Windows
// leaves those queue records behind when a printer moves USB port; removing them
// automates the first half of the old reset-usb-printer.ps1 ritual.
func Cleanup() (CleanupReport, error) {
	if !IsElevated() {
		return CleanupReport{}, ErrNotElevated
	}

	list, err := ListUSB()
	if err != nil {
		return CleanupReport{}, err
	}

	report := CleanupReport{RemovedQueues: []string{}}
	for _, printer := range list {
		if !printer.Stale {
			continue
		}
		if deleteErr := deletePrinter(printer.Name); deleteErr != nil {
			report.FailedQueues = append(report.FailedQueues, printer.Name)
			continue
		}
		report.RemovedQueues = append(report.RemovedQueues, printer.Name)
	}
	return report, nil
}
