package printers

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"time"
)

// chunkSize keeps a large raster logo from filling the printer's receive buffer
// in one write; 4 KiB is what the frontend already assumes for ESC/POS streams.
const chunkSize = 4096

// printNetwork writes raw ESC/POS bytes to a printer that listens on TCP,
// usually port 9100. This is the transport a browser cannot provide.
func printNetwork(ctx context.Context, host string, port uint16, data []byte, timeout time.Duration) (int, error) {
	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	address := net.JoinHostPort(host, strconv.Itoa(int(port)))

	dialer := net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return 0, fmt.Errorf("cannot reach the printer at %s: %w", address, err)
	}
	defer conn.Close()

	if err := conn.SetWriteDeadline(time.Now().Add(timeout)); err != nil {
		return 0, fmt.Errorf("cannot set a write timeout for %s: %w", address, err)
	}

	written := 0
	for offset := 0; offset < len(data); offset += chunkSize {
		end := offset + chunkSize
		if end > len(data) {
			end = len(data)
		}
		n, writeErr := conn.Write(data[offset:end])
		written += n
		if writeErr != nil {
			return written, fmt.Errorf(
				"the printer at %s stopped accepting data after %d bytes: %w",
				address, written, writeErr,
			)
		}
	}

	if tcp, ok := conn.(*net.TCPConn); ok {
		_ = tcp.CloseWrite()
	}
	return written, nil
}
