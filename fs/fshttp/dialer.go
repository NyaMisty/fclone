package fshttp

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"runtime"
	"runtime/debug"
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

func dialTLSContext(ctx context.Context, network, address string, t *http.Transport, ci *fs.ConfigInfo) (net.Conn, error) {
	// Initiate TLS and check remote host name against certificate.
	cfg := &tls.Config{}
	if t.TLSClientConfig != nil {
		cfg = cfg.Clone()
	}
	if cfg.ServerName == "" {
		firstTLSHost, _, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		cfg.ServerName = firstTLSHost
	}
	//if pconn.cacheKey.onlyH1 {
	if ci.DisableHTTP2 {
		cfg.NextProtos = nil
	}
	//plainConn := pconn.conn
	plainConn, err := dialContext(ctx, network, address, ci)
	if err != nil {
		return nil, err
	}
	tlsConn := tls.Client(plainConn, cfg)
	errc := make(chan error, 2)
	var timer *time.Timer // for canceling TLS handshake
	if d := t.TLSHandshakeTimeout; d != 0 {
		timer = time.AfterFunc(d, func() {
			errc <- os.ErrDeadlineExceeded
		})
	}
	go func() {
		//if trace != nil && trace.TLSHandshakeStart != nil {
		//	trace.TLSHandshakeStart()
		//}
		err := tlsConn.HandshakeContext(ctx)
		if timer != nil {
			timer.Stop()
		}
		errc <- err
	}()
	if err := <-errc; err != nil {
		plainConn.Close()
		//if trace != nil && trace.TLSHandshakeDone != nil {
		//	trace.TLSHandshakeDone(tls.ConnectionState{}, err)
		//}
		return nil, err
	}
	return tlsConn, nil
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
	// disable timeoutConn when FastConn engaged
	if dialer.minSpeedDuration != 0 {
		dialer.timeout = 0
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
		Conn:    c,
		timeout: d.timeout,
	}
	return t, t.nudgeDeadline()
}

func (d *Dialer) DialTLSContext(ctx context.Context, network, address string) (net.Conn, error) {
	return NewDialer(ctx).DialContext(ctx, network, address)
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
func FastConnWrap(conn net.Conn, ci *fs.ConfigInfo) net.Conn {
	//return conn
	return &FastConn{
		Conn:                 conn,
		minimumSpeedPerSec:   ci.MinSpeedPerSec,   // 400 * 1024 400k
		minimumSpeedDuration: ci.MinSpeedDuration, //  300 * time.Second
	}
}

type FastConn struct {
	net.Conn
	minimumSpeedPerSec   fs.SizeSuffix
	minimumSpeedDuration time.Duration

	lastOperationTime  time.Time
	curDeadlineStart   time.Time
	curTrafficSum      int64
	curTrafficDuration time.Duration
}

func (c *FastConn) String() string {
	return fmt.Sprintf("[FastConn %v->%v]", c.LocalAddr(), c.RemoteAddr())
}

func (c *FastConn) checkSpeed() (passed bool) {
	if c.curTrafficDuration > c.minimumSpeedDuration {
		c.curTrafficDuration = c.minimumSpeedDuration // a conn may be read&write at same time & overlap
	}
	if c.curTrafficDuration < c.minimumSpeedDuration-time.Second*5 {
		// not been actively used, ignoring
		return true
	}

	curStack := string(debug.Stack())
	if strings.Contains(curStack, "net/http.(*persistConn).readLoop") {
		// this is a readloop, no use
		return true
	}

	//minimumTraffic := int64(0 * 1024) // TLS Keepalive
	// if c.curTrafficSum > minimumTraffic
	if int(c.curTrafficSum) < int(c.minimumSpeedPerSec)*int(c.curTrafficDuration/time.Second) {
		fs.Debugf(nil, "fastconn: Killing connection because speed too low: %s -> %s! startTime: %v, duration: %v, traffic: %d, stack: %s",
			c.Conn.LocalAddr(), c.Conn.RemoteAddr(),
			c.curDeadlineStart, c.curTrafficDuration, c.curTrafficSum, string(debug.Stack()))
		_ = c.Conn.Close()
		return false
	}
	fs.Debugf(nil, "fastconn: Connection Good: %s -> %s, duration: %v, traffic: %d",
		c.Conn.LocalAddr(), c.Conn.RemoteAddr(),
		c.curTrafficDuration, c.curTrafficSum)
	return true
}

func (c *FastConn) doOperation(fun func(b []byte) (n int, err error), b []byte, isWrite bool) (n int, err error) {
	// Directly return if not configured
	if c.minimumSpeedPerSec == 0 {
		return fun(b)
	}

	// Update deadline before request
	if time.Now().After(c.curDeadlineStart.Add(c.minimumSpeedDuration).Add(-time.Second)) {
		// we don't handle idle connections, because connection that aren't blocked are always with hope
		// but maybe some connection get throttled to keep 2kb/s constantly?
		// anyway, leave it for the future :)
		//if !c.checkSpeed() {
		//	return 0, fmt.Errorf("%w, closing conn because the speed is too slow", os.ErrDeadlineExceeded)
		//}
		c.curDeadlineStart = time.Now()
		c.curTrafficSum = 0
		c.curTrafficDuration = 0
		//fs.Debugf(c, "fastconn: updating deadline start %v", c.curDeadlineStart)
	}
	// we set deadline before read/write, BEFORE nudgeDeadline
	//setDeadlineFunc := c.SetReadDeadline
	//if isWrite {
	//	setDeadlineFunc = c.SetWriteDeadline
	//}
	setDeadlineFunc := c.SetDeadline
	if err := setDeadlineFunc(c.curDeadlineStart.Add(c.minimumSpeedDuration)); err != nil {
		fs.LogPrintf(fs.LogLevelWarning, c, "fastconn: Cannot set deadline: %v", err)
	}

	// Do the job and bookkeeping
	startTime := time.Now()
	c.lastOperationTime = startTime
	n, err = fun(b)
	c.curTrafficDuration += time.Now().Sub(startTime)
	c.curTrafficSum += int64(n)

	setDeadlineFunc(time.Time{})

	// check the speed
	if errors.Is(err, os.ErrDeadlineExceeded) {
		if !c.checkSpeed() {
			// passthrough the error
			err = fmt.Errorf("%w, fastconn closing conn because the speed is too slow", err)
		} else {
			err = nil // it's a good connection!
			if isWrite {
				fs.LogPrintf(fs.LogLevelDebug, c, "fastconn: fixed ErrDeadlineExceeded, start: %v, duration: %v, isWrite: %v", c.curDeadlineStart, c.curTrafficDuration, isWrite)
			}
		}
	} else {
		// if a conn didn't deadline here, then it absolutely didn't get to the minimumSpeedDuration
		// so no need to checkSpeed here :)
	}
	return n, err
}

// Write bytes with min traffic
func (c *FastConn) Write(b []byte) (n int, err error) {
	n = 0
	retries := 0
	for {
		var _n int
		_n, err = c.doOperation(func(b []byte) (int, error) {
			return c.Conn.Write(b)
		}, b[n:], true)
		n += _n
		if err != nil {
			break
		}
		if _n > 0 {
			retries = 0
		}
		if n == len(b) {
			break
		} else if n > len(b) {
			panic("write past")
		} else {
			// short write
			fs.LogPrintf(fs.LogLevelDebug, c, "fastconn: partial wrote %d-%d/%d, try to fix!", n-_n, n, len(b))
			retries++
			if retries > 4 {
				err = fmt.Errorf("%w, fastconn failed to fix partial wrote", os.ErrDeadlineExceeded)
				return
			}
			time.Sleep(500 * time.Millisecond)
		}
	}
	return
}

// Read bytes with rate limiting and idle timeouts
func (c *FastConn) Read(b []byte) (n int, err error) {
	return c.doOperation(func(b []byte) (int, error) {
		return c.Conn.Read(b)
	}, b, false)
}
