package proxy

import (
	"net"
	"time"

	"golang.org/x/net/context"
)

func netCopy(ctx context.Context, from net.Conn, to net.Conn, buf []byte, errs chan error) {
	timeout := 5 * time.Second
	for {
		select {
		case <-ctx.Done():
			// If context is canceled, exit
			errs <- nil
			return
		default:
			// Extend reader's deadline
			from.SetReadDeadline(time.Now().Add(timeout))
			// Read data from the source connection.
			read, err := from.Read(buf)
			// If read error occurs, check if it's a fatal error (not a timeout)
			// and stop the proxiying
			if err != nil {
				if isNetTimeout(err) {
					// On deadline exceeded, keep going so we check out
					// on the stop channel.
					continue
				}
				// On error, stop there and notify the caller
				errs <- err
				return
			}

			// Extend reader's deadline
			to.SetWriteDeadline(time.Now().Add(timeout))
			// Write data to the destination.
			_, err = to.Write(buf[:read])
			// If write error occurs, check if it's a fatal error (not a timeout)
			// and stop the proxiying
			if err != nil {
				if isNetTimeout(err) {
					// On deadline exceeded, keep going so we check out
					// on the stop channel.
					continue
				}
				errs <- err
				return
			}
		}
	}
}

func isNetTimeout(err error) bool {
	if err, ok := err.(net.Error); ok {
		return err.Timeout()
	}
	return false
}
