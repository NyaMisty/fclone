package fshttp

import (
	"context"
	"errors"
	"net"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/accounting"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

func dialContext(ctx context.Context, network, address string, ci *fs.ConfigInfo) (net.Conn, error) {
	return NewDialer(ctx).DialContext(ctx, network, address)
}

// Dialer structure contains default dialer and timeout, tclass support
type Dialer struct {
	net.Dialer
	timeout time.Duration
	tclass  int

	minSpeedPerSec   fs.SizeSuffix
	minSpeedDuration time.Duration
}

// NewDialer creates a Dialer structure with Timeout, Keepalive,
// LocalAddr and DSCP set from rclone flags.
func NewDialer(ctx context.Context) *Dialer {
	ci := fs.GetConfig(ctx)
	dialer := &Dialer{
		Dialer: net.Dialer{
			Timeout:   ci.ConnectTimeout,
			KeepAlive: 30 * time.Second,
		},
		timeout: ci.Timeout,
		tclass:  int(ci.TrafficClass),

		minSpeedPerSec:   ci.MinSpeedPerSec,
		minSpeedDuration: ci.MinSpeedDuration,
	}
	if ci.BindAddr != nil {
		dialer.Dialer.LocalAddr = &net.TCPAddr{IP: ci.BindAddr}
	}
	return dialer
}

// Dial connects to the network address.
func (d *Dialer) Dial(network, address string) (net.Conn, error) {
	return d.DialContext(context.Background(), network, address)
}

var warnDSCPFail, warnDSCPWindows sync.Once

// DialContext connects to the network address using the provided context.
func (d *Dialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	c, err := d.Dialer.DialContext(ctx, network, address)
	if err != nil {
		return c, err
	}

	if d.tclass != 0 {
		// IPv6 addresses must have two or more ":"
		if strings.Count(c.RemoteAddr().String(), ":") > 1 {
			err = ipv6.NewConn(c).SetTrafficClass(d.tclass)
		} else {
			err = ipv4.NewConn(c).SetTOS(d.tclass)
			// Warn of silent failure on Windows (IPv4 only, IPv6 caught by error handler)
			if runtime.GOOS == "windows" {
				warnDSCPWindows.Do(func() {
					fs.LogLevelPrintf(fs.LogLevelWarning, nil, "dialer: setting DSCP on Windows/IPv4 fails silently; see https://github.com/golang/go/issues/42728")
				})
			}
		}
		if err != nil {
			warnDSCPFail.Do(func() {
				fs.LogLevelPrintf(fs.LogLevelWarning, nil, "dialer: failed to set DSCP socket options: %v", err)
			})
		}
	}

	t := &timeoutConn{
		Conn: &FastConn{
			Conn:                 c,
			minimumSpeedPerSec:   d.minSpeedPerSec,   // 400 * 1024 400k
			minimumSpeedDuration: d.minSpeedDuration, //  300 * time.Second
		},
		timeout: d.timeout,
	}
	return t, t.nudgeDeadline()
}

// A net.Conn that sets deadline for every Read/Write operation
type timeoutConn struct {
	net.Conn
	timeout time.Duration
}

// Nudge the deadline for an idle timeout on by c.timeout if non-zero
func (c *timeoutConn) nudgeDeadline() error {
	if c.timeout > 0 {
		return c.SetDeadline(time.Now().Add(c.timeout))
	}
	return nil
}

// Read bytes with rate limiting and idle timeouts
func (c *timeoutConn) Read(b []byte) (n int, err error) {
	// Ideally we would LimitBandwidth(len(b)) here and replace tokens we didn't use
	n, err = c.Conn.Read(b)
	accounting.TokenBucket.LimitBandwidth(accounting.TokenBucketSlotTransportRx, n)
	if err == nil && n > 0 && c.timeout > 0 {
		err = c.nudgeDeadline()
	}
	return n, err
}

// Write bytes with rate limiting and idle timeouts
func (c *timeoutConn) Write(b []byte) (n int, err error) {
	accounting.TokenBucket.LimitBandwidth(accounting.TokenBucketSlotTransportTx, len(b))
	n, err = c.Conn.Write(b)
	if err == nil && n > 0 && c.timeout > 0 {
		err = c.nudgeDeadline()
	}
	return n, err
}

// Mod
type FastConn struct {
	net.Conn
	minimumSpeedPerSec   fs.SizeSuffix
	minimumSpeedDuration time.Duration

	curDeadlineStart   time.Time
	curTrafficSum      int64
	curTrafficDuration time.Duration
}

// Read bytes with rate limiting and idle timeouts
func (c *FastConn) Read(b []byte) (n int, err error) {
	// Ideally we would LimitBandwidth(len(b)) here and replace tokens we didn't use
	if c.minimumSpeedPerSec == 0 {
		return c.Conn.Read(b)
	}
	if time.Now().After(c.curDeadlineStart.Add(c.minimumSpeedDuration)) {
		c.curDeadlineStart = time.Now()
		c.curTrafficSum = 0
		c.curTrafficDuration = 0
	}
	c.SetReadDeadline(c.curDeadlineStart.Add(c.minimumSpeedDuration))
	startTime := time.Now()
	n, err = c.Conn.Read(b)
	c.curTrafficDuration += time.Now().Sub(startTime)
	c.curTrafficSum += int64(n)
	if errors.Is(err, os.ErrDeadlineExceeded) {
		minimumTraffic := int64(2 * 1024) // TLS Keepalive
		if c.curTrafficSum > minimumTraffic && int(c.curTrafficSum) < int(c.minimumSpeedPerSec)*int(c.curTrafficDuration/time.Second) {
			fs.Debugf(nil, "Killing connection because speed too low! startTime: %v, duration: %v, traffic: %d", c.curDeadlineStart, c.curTrafficDuration, c.curTrafficSum)
			_ = c.Conn.Close()
		}
		//err = nil // passthrough the deadline error
	}
	return n, err
}
