// Package sftp provides a filesystem interface using github.com/pkg/sftp

//go:build !plan9
// +build !plan9

package sftp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pkg/sftp"
	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/accounting"
	"github.com/rclone/rclone/fs/config"
	"github.com/rclone/rclone/fs/config/configmap"
	"github.com/rclone/rclone/fs/config/configstruct"
	"github.com/rclone/rclone/fs/config/obscure"
	"github.com/rclone/rclone/fs/fshttp"
	"github.com/rclone/rclone/fs/hash"
	"github.com/rclone/rclone/lib/env"
	"github.com/rclone/rclone/lib/pacer"
	"github.com/rclone/rclone/lib/readers"
	sshagent "github.com/xanzy/ssh-agent"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

const (
	hashCommandNotSupported = "none"
	minSleep                = 100 * time.Millisecond
	maxSleep                = 2 * time.Second
	decayConstant           = 2 // bigger for slower decay, exponential
)

var (
	currentUser = env.CurrentUser()
)

func init() {
	fsi := &fs.RegInfo{
		Name:        "sftp",
		Description: "SSH/SFTP Connection",
		NewFs:       NewFs,
		Options: []fs.Option{{
			Name:     "host",
			Help:     "SSH host to connect to.\n\nE.g. \"example.com\".",
			Required: true,
		}, {
			Name: "user",
			Help: "SSH username, leave blank for current username, " + currentUser + ".",
		}, {
			Name: "port",
			Help: "SSH port, leave blank to use default (22).",
		}, {
			Name:       "pass",
			Help:       "SSH password, leave blank to use ssh-agent.",
			IsPassword: true,
		}, {
			Name: "key_pem",
			Help: "Raw PEM-encoded private key.\n\nIf specified, will override key_file parameter.",
		}, {
			Name: "key_file",
			Help: "Path to PEM-encoded private key file.\n\nLeave blank or set key-use-agent to use ssh-agent." + env.ShellExpandHelp,
		}, {
			Name: "key_file_pass",
			Help: `The passphrase to decrypt the PEM-encoded private key file.

Only PEM encrypted key files (old OpenSSH format) are supported. Encrypted keys
in the new OpenSSH format can't be used.`,
			IsPassword: true,
		}, {
			Name: "pubkey_file",
			Help: `Optional path to public key file.

Set this if you have a signed certificate you want to use for authentication.` + env.ShellExpandHelp,
		}, {
			Name: "known_hosts_file",
			Help: `Optional path to known_hosts file.

Set this value to enable server host key validation.` + env.ShellExpandHelp,
			Advanced: true,
			Examples: []fs.OptionExample{{
				Value: "~/.ssh/known_hosts",
				Help:  "Use OpenSSH's known_hosts file.",
			}},
		}, {
			Name: "key_use_agent",
			Help: `When set forces the usage of the ssh-agent.

When key-file is also set, the ".pub" file of the specified key-file is read and only the associated key is
requested from the ssh-agent. This allows to avoid ` + "`Too many authentication failures for *username*`" + ` errors
when the ssh-agent contains many keys.`,
			Default: false,
		}, {
			Name: "use_insecure_cipher",
			Help: `Enable the use of insecure ciphers and key exchange methods. 

This enables the use of the following insecure ciphers and key exchange methods:

- aes128-cbc
- aes192-cbc
- aes256-cbc
- 3des-cbc
- diffie-hellman-group-exchange-sha256
- diffie-hellman-group-exchange-sha1

Those algorithms are insecure and may allow plaintext data to be recovered by an attacker.`,
			Default: false,
			Examples: []fs.OptionExample{
				{
					Value: "false",
					Help:  "Use default Cipher list.",
				}, {
					Value: "true",
					Help:  "Enables the use of the aes128-cbc cipher and diffie-hellman-group-exchange-sha256, diffie-hellman-group-exchange-sha1 key exchange.",
				},
			},
		}, {
			Name:    "disable_hashcheck",
			Default: false,
			Help:    "Disable the execution of SSH commands to determine if remote file hashing is available.\n\nLeave blank or set to false to enable hashing (recommended), set to true to disable hashing.",
		}, {
			Name:    "ask_password",
			Default: false,
			Help: `Allow asking for SFTP password when needed.

If this is set and no password is supplied then rclone will:
- ask for a password
- not contact the ssh agent
`,
			Advanced: true,
		}, {
			Name:    "path_override",
			Default: "",
			Help: `Override path used by SSH connection.

This allows checksum calculation when SFTP and SSH paths are
different. This issue affects among others Synology NAS boxes.

Shared folders can be found in directories representing volumes

    rclone sync /home/local/directory remote:/directory --sftp-path-override /volume2/directory

Home directory can be found in a shared folder called "home"

    rclone sync /home/local/directory remote:/home/directory --sftp-path-override /volume1/homes/USER/directory`,
			Advanced: true,
		}, {
			Name:     "set_modtime",
			Default:  true,
			Help:     "Set the modified time on the remote if set.",
			Advanced: true,
		}, {
			Name:     "md5sum_command",
			Default:  "",
			Help:     "The command used to read md5 hashes.\n\nLeave blank for autodetect.",
			Advanced: true,
		}, {
			Name:     "sha1sum_command",
			Default:  "",
			Help:     "The command used to read sha1 hashes.\n\nLeave blank for autodetect.",
			Advanced: true,
		}, {
			Name:     "skip_links",
			Default:  false,
			Help:     "Set to skip any symlinks and any other non regular files.",
			Advanced: true,
		}, {
			Name:     "subsystem",
			Default:  "sftp",
			Help:     "Specifies the SSH2 subsystem on the remote host.",
			Advanced: true,
		}, {
			Name:    "server_command",
			Default: "",
			Help: `Specifies the path or command to run a sftp server on the remote host.

The subsystem option is ignored when server_command is defined.`,
			Advanced: true,
		}, {
			Name:    "use_fstat",
			Default: false,
			Help: `If set use fstat instead of stat.

Some servers limit the amount of open files and calling Stat after opening
the file will throw an error from the server. Setting this flag will call
Fstat instead of Stat which is called on an already open file handle.

It has been found that this helps with IBM Sterling SFTP servers which have
"extractability" level set to 1 which means only 1 file can be opened at
any given time.
`,
			Advanced: true,
		}, {
			Name:    "disable_concurrent_reads",
			Default: false,
			Help: `If set don't use concurrent reads.

Normally concurrent reads are safe to use and not using them will
degrade performance, so this option is disabled by default.

Some servers limit the amount number of times a file can be
downloaded. Using concurrent reads can trigger this limit, so if you
have a server which returns

    Failed to copy: file does not exist

Then you may need to enable this flag.

If concurrent reads are disabled, the use_fstat option is ignored.
`,
			Advanced: true,
		}, {
			Name:    "disable_concurrent_writes",
			Default: false,
			Help: `If set don't use concurrent writes.

Normally rclone uses concurrent writes to upload files. This improves
the performance greatly, especially for distant servers.

This option disables concurrent writes should that be necessary.
`,
			Advanced: true,
		}, {
			Name:    "idle_timeout",
			Default: fs.Duration(60 * time.Second),
			Help: `Max time before closing idle connections.

If no connections have been returned to the connection pool in the time
given, rclone will empty the connection pool.

Set to 0 to keep connections indefinitely.
`,
			Advanced: true,
		}},
	}
	fs.Register(fsi)
}

// Options defines the configuration for this backend
type Options struct {
	Host                    string      `config:"host"`
	User                    string      `config:"user"`
	Port                    string      `config:"port"`
	Pass                    string      `config:"pass"`
	KeyPem                  string      `config:"key_pem"`
	KeyFile                 string      `config:"key_file"`
	KeyFilePass             string      `config:"key_file_pass"`
	PubKeyFile              string      `config:"pubkey_file"`
	KnownHostsFile          string      `config:"known_hosts_file"`
	KeyUseAgent             bool        `config:"key_use_agent"`
	UseInsecureCipher       bool        `config:"use_insecure_cipher"`
	DisableHashCheck        bool        `config:"disable_hashcheck"`
	AskPassword             bool        `config:"ask_password"`
	PathOverride            string      `config:"path_override"`
	SetModTime              bool        `config:"set_modtime"`
	Md5sumCommand           string      `config:"md5sum_command"`
	Sha1sumCommand          string      `config:"sha1sum_command"`
	SkipLinks               bool        `config:"skip_links"`
	Subsystem               string      `config:"subsystem"`
	ServerCommand           string      `config:"server_command"`
	UseFstat                bool        `config:"use_fstat"`
	DisableConcurrentReads  bool        `config:"disable_concurrent_reads"`
	DisableConcurrentWrites bool        `config:"disable_concurrent_writes"`
	IdleTimeout             fs.Duration `config:"idle_timeout"`
}

// Fs stores the interface to the remote SFTP files
type Fs struct {
	name         string
	root         string
	absRoot      string
	opt          Options          // parsed options
	ci           *fs.ConfigInfo   // global config
	m            configmap.Mapper // config
	features     *fs.Features     // optional features
	config       *ssh.ClientConfig
	url          string
	mkdirLock    *stringLock
	cachedHashes *hash.Set
	poolMu       sync.Mutex
	pool         []*conn
	drain        *time.Timer // used to drain the pool when we stop using the connections
	pacer        *fs.Pacer   // pacer for operations
	savedpswd    string
	sessions     int32 // count in use sessions
}

// Object is a remote SFTP file that has been stat'd (so it exists, but is not necessarily open for reading)
type Object struct {
	fs      *Fs
	remote  string
	size    int64       // size of the object
	modTime time.Time   // modification time of the object
	mode    os.FileMode // mode bits from the file
	md5sum  *string     // Cached MD5 checksum
	sha1sum *string     // Cached SHA1 checksum
}

// dial starts a client connection to the given SSH server. It is a
// convenience function that connects to the given network address,
// initiates the SSH handshake, and then sets up a Client.
func (f *Fs) dial(ctx context.Context, network, addr string, sshConfig *ssh.ClientConfig) (*ssh.Client, error) {
	dialer := fshttp.NewDialer(ctx)
	conn, err := dialer.Dial(network, addr)
	if err != nil {
		return nil, err
	}
	c, chans, reqs, err := ssh.NewClientConn(conn, addr, sshConfig)
	if err != nil {
		return nil, err
	}
	fs.Debugf(f, "New connection %s->%s to %q", c.LocalAddr(), c.RemoteAddr(), c.ServerVersion())
	return ssh.NewClient(c, chans, reqs), nil
}

// conn encapsulates an ssh client and corresponding sftp client
type conn struct {
	sshClient  *ssh.Client
	sftpClient *sftp.Client
	err        chan error
}

// Wait for connection to close
func (c *conn) wait() {
	c.err <- c.sshClient.Conn.Wait()
}

// Closes the connection
func (c *conn) close() error {
	sftpErr := c.sftpClient.Close()
	sshErr := c.sshClient.Close()
	if sftpErr != nil {
		return sftpErr
	}
	return sshErr
}

// Returns an error if closed
func (c *conn) closed() error {
	select {
	case err := <-c.err:
		return err
	default:
	}
	return nil
}

// Show that we are using an ssh session
//
// Call removeSession() when done
func (f *Fs) addSession() {
	atomic.AddInt32(&f.sessions, 1)
}

// Show the ssh session is no longer in use
func (f *Fs) removeSession() {
	atomic.AddInt32(&f.sessions, -1)
}

// getSessions shows whether there are any sessions in use
func (f *Fs) getSessions() int32 {
	return atomic.LoadInt32(&f.sessions)
}

// Open a new connection to the SFTP server.
func (f *Fs) sftpConnection(ctx context.Context) (c *conn, err error) {
	// Rate limit rate of new connections
	c = &conn{
		err: make(chan error, 1),
	}
	c.sshClient, err = f.dial(ctx, "tcp", f.opt.Host+":"+f.opt.Port, f.config)
	if err != nil {
		return nil, fmt.Errorf("couldn't connect SSH: %w", err)
	}
	c.sftpClient, err = f.newSftpClient(c.sshClient)
	if err != nil {
		_ = c.sshClient.Close()
		return nil, fmt.Errorf("couldn't initialise SFTP: %w", err)
	}
	go c.wait()
	return c, nil
}

// Creates a new SFTP client on conn, using the specified subsystem
// or sftp server, and zero or more option functions
func (f *Fs) newSftpClient(conn *ssh.Client, opts ...sftp.ClientOption) (*sftp.Client, error) {
	s, err := conn.NewSession()
	if err != nil {
		return nil, err
	}
	pw, err := s.StdinPipe()
	if err != nil {
		return nil, err
	}
	pr, err := s.StdoutPipe()
	if err != nil {
		return nil, err
	}

	if f.opt.ServerCommand != "" {
		if err := s.Start(f.opt.ServerCommand); err != nil {
			return nil, err
		}
	} else {
		if err := s.RequestSubsystem(f.opt.Subsystem); err != nil {
			return nil, err
		}
	}
	opts = opts[:len(opts):len(opts)] // make sure we don't overwrite the callers opts
	opts = append(opts,
		sftp.UseFstat(f.opt.UseFstat),
		sftp.UseConcurrentReads(!f.opt.DisableConcurrentReads),
		sftp.UseConcurrentWrites(!f.opt.DisableConcurrentWrites),
	)
	return sftp.NewClientPipe(pr, pw, opts...)
}

// Get an SFTP connection from the pool, or open a new one
func (f *Fs) getSftpConnection(ctx context.Context) (c *conn, err error) {
	accounting.LimitTPS(ctx)
	f.poolMu.Lock()
	for len(f.pool) > 0 {
		c = f.pool[0]
		f.pool = f.pool[1:]
		err := c.closed()
		if err == nil {
			break
		}
		fs.Errorf(f, "Discarding closed SSH connection: %v", err)
		c = nil
	}
	f.poolMu.Unlock()
	if c != nil {
		return c, nil
	}
	err = f.pacer.Call(func() (bool, error) {
		c, err = f.sftpConnection(ctx)
		if err != nil {
			return true, err
		}
		return false, nil
	})
	return c, err
}

// Return an SFTP connection to the pool
//
// It nils the pointed to connection out so it can't be reused
//
// if err is not nil then it checks the connection is alive using a
// Getwd request
func (f *Fs) putSftpConnection(pc **conn, err error) {
	c := *pc
	*pc = nil
	if err != nil {
		// work out if this is an expected error
		isRegularError := false
		var statusErr *sftp.StatusError
		var pathErr *os.PathError
		switch {
		case errors.Is(err, os.ErrNotExist):
			isRegularError = true
		case errors.As(err, &statusErr):
			isRegularError = true
		case errors.As(err, &pathErr):
			isRegularError = true
		}
		// If not a regular SFTP error code then check the connection
		if !isRegularError {
			_, nopErr := c.sftpClient.Getwd()
			if nopErr != nil {
				fs.Debugf(f, "Connection failed, closing: %v", nopErr)
				_ = c.close()
				return
			}
			fs.Debugf(f, "Connection OK after error: %v", err)
		}
	}
	f.poolMu.Lock()
	f.pool = append(f.pool, c)
	if f.opt.IdleTimeout > 0 {
		f.drain.Reset(time.Duration(f.opt.IdleTimeout)) // nudge on the pool emptying timer
	}
	f.poolMu.Unlock()
}

// Drain the pool of any connections
func (f *Fs) drainPool(ctx context.Context) (err error) {
	f.poolMu.Lock()
	defer f.poolMu.Unlock()
	if sessions := f.getSessions(); sessions != 0 {
		fs.Debugf(f, "Not closing %d unused connections as %d sessions active", len(f.pool), sessions)
		if f.opt.IdleTimeout > 0 {
			f.drain.Reset(time.Duration(f.opt.IdleTimeout)) // nudge on the pool emptying timer
		}
		return nil
	}
	if f.opt.IdleTimeout > 0 {
		f.drain.Stop()
	}
	if len(f.pool) != 0 {
		fs.Debugf(f, "closing %d unused connections", len(f.pool))
	}
	for i, c := range f.pool {
		if cErr := c.closed(); cErr == nil {
			cErr = c.close()
			if cErr != nil {
				err = cErr
			}
		}
		f.pool[i] = nil
	}
	f.pool = nil
	return err
}

// NewFs creates a new Fs object from the name and root. It connects to
// the host specified in the config file.
func NewFs(ctx context.Context, name, root string, m configmap.Mapper) (fs.Fs, error) {
	// This will hold the Fs object.  We need to create it here
	// so we can refer to it in the SSH callback, but it's populated
	// in NewFsWithConnection
	f := &Fs{
		ci: fs.GetConfig(ctx),
	}
	// Parse config into Options struct
	opt := new(Options)
	err := configstruct.Set(m, opt)
	if err != nil {
		return nil, err
	}
	if opt.User == "" {
		opt.User = currentUser
	}
	if opt.Port == "" {
		opt.Port = "22"
	}

	sshConfig := &ssh.ClientConfig{
		User:            opt.User,
		Auth:            []ssh.AuthMethod{},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         f.ci.ConnectTimeout,
		ClientVersion:   "SSH-2.0-" + f.ci.UserAgent,
	}

	if opt.KnownHostsFile != "" {
		hostcallback, err := knownhosts.New(env.ShellExpand(opt.KnownHostsFile))
		if err != nil {
			return nil, fmt.Errorf("couldn't parse known_hosts_file: %w", err)
		}
		sshConfig.HostKeyCallback = hostcallback
	}

	if opt.UseInsecureCipher {
		sshConfig.Config.SetDefaults()
		sshConfig.Config.Ciphers = append(sshConfig.Config.Ciphers, "aes128-cbc", "aes192-cbc", "aes256-cbc", "3des-cbc")
		sshConfig.Config.KeyExchanges = append(sshConfig.Config.KeyExchanges, "diffie-hellman-group-exchange-sha1", "diffie-hellman-group-exchange-sha256")
	}

	keyFile := env.ShellExpand(opt.KeyFile)
	pubkeyFile := env.ShellExpand(opt.PubKeyFile)
	//keyPem := env.ShellExpand(opt.KeyPem)
	// Add ssh agent-auth if no password or file or key PEM specified
	if (opt.Pass == "" && keyFile == "" && !opt.AskPassword && opt.KeyPem == "") || opt.KeyUseAgent {
		sshAgentClient, _, err := sshagent.New()
		if err != nil {
			return nil, fmt.Errorf("couldn't connect to ssh-agent: %w", err)
		}
		signers, err := sshAgentClient.Signers()
		if err != nil {
			return nil, fmt.Errorf("couldn't read ssh agent signers: %w", err)
		}
		if keyFile != "" {
			pubBytes, err := ioutil.ReadFile(keyFile + ".pub")
			if err != nil {
				return nil, fmt.Errorf("failed to read public key file: %w", err)
			}
			pub, _, _, _, err := ssh.ParseAuthorizedKey(pubBytes)
			if err != nil {
				return nil, fmt.Errorf("failed to parse public key file: %w", err)
			}
			pubM := pub.Marshal()
			found := false
			for _, s := range signers {
				if bytes.Equal(pubM, s.PublicKey().Marshal()) {
					sshConfig.Auth = append(sshConfig.Auth, ssh.PublicKeys(s))
					found = true
					break
				}
			}
			if !found {
				return nil, errors.New("private key not found in the ssh-agent")
			}
		} else {
			sshConfig.Auth = append(sshConfig.Auth, ssh.PublicKeys(signers...))
		}
	}

	// Load key file if specified
	if keyFile != "" || opt.KeyPem != "" {
		var key []byte
		if opt.KeyPem == "" {
			key, err = ioutil.ReadFile(keyFile)
			if err != nil {
				return nil, fmt.Errorf("failed to read private key file: %w", err)
			}
		} else {
			// wrap in quotes because the config is a coming as a literal without them.
			opt.KeyPem, err = strconv.Unquote("\"" + opt.KeyPem + "\"")
			if err != nil {
				return nil, fmt.Errorf("pem key not formatted properly: %w", err)
			}
			key = []byte(opt.KeyPem)
		}
		clearpass := ""
		if opt.KeyFilePass != "" {
			clearpass, err = obscure.Reveal(opt.KeyFilePass)
			if err != nil {
				return nil, err
			}
		}
		var signer ssh.Signer
		if clearpass == "" {
			signer, err = ssh.ParsePrivateKey(key)
		} else {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(key, []byte(clearpass))
		}
		if err != nil {
			return nil, fmt.Errorf("failed to parse private key file: %w", err)
		}

		// If a public key has been specified then use that
		if pubkeyFile != "" {
			certfile, err := ioutil.ReadFile(pubkeyFile)
			if err != nil {
				return nil, fmt.Errorf("unable to read cert file: %w", err)
			}

			pk, _, _, _, err := ssh.ParseAuthorizedKey(certfile)
			if err != nil {
				return nil, fmt.Errorf("unable to parse cert file: %w", err)
			}

			// And the signer for this, which includes the private key signer
			// This is what we'll pass to the ssh client.
			// Normally the ssh client will use the public key built
			// into the private key, but we need to tell it to use the user
			// specified public key cert.  This signer is specific to the
			// cert and will include the private key signer.  Now ssh
			// knows everything it needs.
			cert, ok := pk.(*ssh.Certificate)
			if !ok {
				return nil, errors.New("public key file is not a certificate file: " + pubkeyFile)
			}
			pubsigner, err := ssh.NewCertSigner(cert, signer)
			if err != nil {
				return nil, fmt.Errorf("error generating cert signer: %w", err)
			}
			sshConfig.Auth = append(sshConfig.Auth, ssh.PublicKeys(pubsigner))
		} else {
			sshConfig.Auth = append(sshConfig.Auth, ssh.PublicKeys(signer))
		}
	}

	// Auth from password if specified
	if opt.Pass != "" {
		clearpass, err := obscure.Reveal(opt.Pass)
		if err != nil {
			return nil, err
		}
		sshConfig.Auth = append(sshConfig.Auth,
			ssh.Password(clearpass),
			ssh.KeyboardInteractive(func(user, instruction string, questions []string, echos []bool) ([]string, error) {
				return f.keyboardInteractiveReponse(user, instruction, questions, echos, clearpass)
			}),
		)
	}

	// Config for password if none was defined and we're allowed to
	// We don't ask now; we ask if the ssh connection succeeds
	if opt.Pass == "" && opt.AskPassword {
		sshConfig.Auth = append(sshConfig.Auth,
			ssh.PasswordCallback(f.getPass),
			ssh.KeyboardInteractive(func(user, instruction string, questions []string, echos []bool) ([]string, error) {
				pass, _ := f.getPass()
				return f.keyboardInteractiveReponse(user, instruction, questions, echos, pass)
			}),
		)
	}

	return NewFsWithConnection(ctx, f, name, root, m, opt, sshConfig)
}

// Do the keyboard interactive challenge
//
// Just send the password back for all questions
func (f *Fs) keyboardInteractiveReponse(user, instruction string, questions []string, echos []bool, pass string) ([]string, error) {
	fs.Debugf(f, "keyboard interactive auth requested")
	answers := make([]string, len(questions))
	for i := range answers {
		answers[i] = pass
	}
	return answers, nil
}

// If we're in password mode and ssh connection succeeds then this
// callback is called.  First time around we ask the user, and then
// save it so on reconnection we give back the previous string.
// This removes the ability to let the user correct a mistaken entry,
// but means that reconnects are transparent.
// We'll re-use config.Pass for this, 'cos we know it's not been
// specified.
func (f *Fs) getPass() (string, error) {
	for f.savedpswd == "" {
		_, _ = fmt.Fprint(os.Stderr, "Enter SFTP password: ")
		f.savedpswd = config.ReadPassword()
	}
	return f.savedpswd, nil
}

// NewFsWithConnection creates a new Fs object from the name and root and an ssh.ClientConfig. It connects to
// the host specified in the ssh.ClientConfig
func NewFsWithConnection(ctx context.Context, f *Fs, name string, root string, m configmap.Mapper, opt *Options, sshConfig *ssh.ClientConfig) (fs.Fs, error) {
	// Populate the Filesystem Object
	f.name = name
	f.root = root
	f.absRoot = root
	f.opt = *opt
	f.m = m
	f.config = sshConfig
	f.url = "sftp://" + opt.User + "@" + opt.Host + ":" + opt.Port + "/" + root
	f.mkdirLock = newStringLock()
	f.pacer = fs.NewPacer(ctx, pacer.NewDefault(pacer.MinSleep(minSleep), pacer.MaxSleep(maxSleep), pacer.DecayConstant(decayConstant)))
	f.savedpswd = ""
	// set the pool drainer timer going
	if f.opt.IdleTimeout > 0 {
		f.drain = time.AfterFunc(time.Duration(opt.IdleTimeout), func() { _ = f.drainPool(ctx) })
	}

	f.features = (&fs.Features{
		CanHaveEmptyDirectories: true,
		SlowHash:                true,
	}).Fill(ctx, f)
	// Make a connection and pool it to return errors early
	c, err := f.getSftpConnection(ctx)
	if err != nil {
		return nil, fmt.Errorf("NewFs: %w", err)
	}
	cwd, err := c.sftpClient.Getwd()
	f.putSftpConnection(&c, nil)
	if err != nil {
		fs.Debugf(f, "Failed to read current directory - using relative paths: %v", err)
	} else if !path.IsAbs(f.root) {
		f.absRoot = path.Join(cwd, f.root)
		fs.Debugf(f, "Using absolute root directory %q", f.absRoot)
	}
	if root != "" {
		// Check to see if the root actually an existing file
		oldAbsRoot := f.absRoot
		remote := path.Base(root)
		f.root = path.Dir(root)
		f.absRoot = path.Dir(f.absRoot)
		if f.root == "." {
			f.root = ""
		}
		_, err := f.NewObject(ctx, remote)
		if err != nil {
			if err == fs.ErrorObjectNotFound || err == fs.ErrorIsDir {
				// File doesn't exist so return old f
				f.root = root
				f.absRoot = oldAbsRoot
				return f, nil
			}
			return nil, err
		}
		// return an error with an fs which points to the parent
		return f, fs.ErrorIsFile
	}
	return f, nil
}

// Name returns the configured name of the file system
func (f *Fs) Name() string {
	return f.name
}

// Root returns the root for the filesystem
func (f *Fs) Root() string {
	return f.root
}

// String returns the URL for the filesystem
func (f *Fs) String() string {
	return f.url
}

// Features returns the optional features of this Fs
func (f *Fs) Features() *fs.Features {
	return f.features
}

// Precision is the remote sftp file system's modtime precision, which we have no way of knowing. We estimate at 1s
func (f *Fs) Precision() time.Duration {
	return time.Second
}

// NewObject creates a new remote sftp file object
func (f *Fs) NewObject(ctx context.Context, remote string) (fs.Object, error) {
	o := &Object{
		fs:     f,
		remote: remote,
	}
	err := o.stat(ctx)
	if err != nil {
		return nil, err
	}
	return o, nil
}

// dirExists returns true,nil if the directory exists, false, nil if
// it doesn't or false, err
func (f *Fs) dirExists(ctx context.Context, dir string) (bool, error) {
	if dir == "" {
		dir = "."
	}
	c, err := f.getSftpConnection(ctx)
	if err != nil {
		return false, fmt.Errorf("dirExists: %w", err)
	}
	info, err := c.sftpClient.Stat(dir)
	f.putSftpConnection(&c, err)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("dirExists stat failed: %w", err)
	}
	if !info.IsDir() {
		return false, fs.ErrorIsFile
	}
	return true, nil
}

// List the objects and directories in dir into entries.  The
// entries can be returned in any order but should be for a
// complete directory.
//
// dir should be "" to list the root, and should not have
// trailing slashes.
//
// This should return ErrDirNotFound if the directory isn't
// found.
func (f *Fs) List(ctx context.Context, dir string) (entries fs.DirEntries, err error) {
	root := path.Join(f.absRoot, dir)
	ok, err := f.dirExists(ctx, root)
	if err != nil {
		return nil, fmt.Errorf("List failed: %w", err)
	}
	if !ok {
		return nil, fs.ErrorDirNotFound
	}
	sftpDir := root
	if sftpDir == "" {
		sftpDir = "."
	}
	c, err := f.getSftpConnection(ctx)
	if err != nil {
		return nil, fmt.Errorf("List: %w", err)
	}
	infos, err := c.sftpClient.ReadDir(sftpDir)
	f.putSftpConnection(&c, err)
	if err != nil {
		return nil, fmt.Errorf("error listing %q: %w", dir, err)
	}
	for _, info := range infos {
		remote := path.Join(dir, info.Name())
		// If file is a symlink (not a regular file is the best cross platform test we can do), do a stat to
		// pick up the size and type of the destination, instead of the size and type of the symlink.
		if !info.Mode().IsRegular() && !info.IsDir() {
			if f.opt.SkipLinks {
				// skip non regular file if SkipLinks is set
				continue
			}
			oldInfo := info
			info, err = f.stat(ctx, remote)
			if err != nil {
				if !os.IsNotExist(err) {
					fs.Errorf(remote, "stat of non-regular file failed: %v", err)
				}
				info = oldInfo
			}
		}
		if info.IsDir() {
			d := fs.NewDir(remote, info.ModTime())
			entries = append(entries, d)
		} else {
			o := &Object{
				fs:     f,
				remote: remote,
			}
			o.setMetadata(info)
			entries = append(entries, o)
		}
	}
	return entries, nil
}

// Put data from <in> into a new remote sftp file object described by <src.Remote()> and <src.ModTime(ctx)>
func (f *Fs) Put(ctx context.Context, in io.Reader, src fs.ObjectInfo, options ...fs.OpenOption) (fs.Object, error) {
	err := f.mkParentDir(ctx, src.Remote())
	if err != nil {
		return nil, fmt.Errorf("Put mkParentDir failed: %w", err)
	}
	// Temporary object under construction
	o := &Object{
		fs:     f,
		remote: src.Remote(),
	}
	err = o.Update(ctx, in, src, options...)
	if err != nil {
		return nil, err
	}
	return o, nil
}

// PutStream uploads to the remote path with the modTime given of indeterminate size
func (f *Fs) PutStream(ctx context.Context, in io.Reader, src fs.ObjectInfo, options ...fs.OpenOption) (fs.Object, error) {
	return f.Put(ctx, in, src, options...)
}

// mkParentDir makes the parent of remote if necessary and any
// directories above that
func (f *Fs) mkParentDir(ctx context.Context, remote string) error {
	parent := path.Dir(remote)
	return f.mkdir(ctx, path.Join(f.absRoot, parent))
}

// mkdir makes the directory and parents using native paths
func (f *Fs) mkdir(ctx context.Context, dirPath string) error {
	f.mkdirLock.Lock(dirPath)
	defer f.mkdirLock.Unlock(dirPath)
	if dirPath == "." || dirPath == "/" {
		return nil
	}
	ok, err := f.dirExists(ctx, dirPath)
	if err != nil {
		return fmt.Errorf("mkdir dirExists failed: %w", err)
	}
	if ok {
		return nil
	}
	parent := path.Dir(dirPath)
	err = f.mkdir(ctx, parent)
	if err != nil {
		return err
	}
	c, err := f.getSftpConnection(ctx)
	if err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	err = c.sftpClient.Mkdir(dirPath)
	f.putSftpConnection(&c, err)
	if err != nil {
		return fmt.Errorf("mkdir %q failed: %w", dirPath, err)
	}
	return nil
}

// Mkdir makes the root directory of the Fs object
func (f *Fs) Mkdir(ctx context.Context, dir string) error {
	root := path.Join(f.absRoot, dir)
	return f.mkdir(ctx, root)
}

// Rmdir removes the root directory of the Fs object
func (f *Fs) Rmdir(ctx context.Context, dir string) error {
	// Check to see if directory is empty as some servers will
	// delete recursively with RemoveDirectory
	entries, err := f.List(ctx, dir)
	if err != nil {
		return fmt.Errorf("Rmdir: %w", err)
	}
	if len(entries) != 0 {
		return fs.ErrorDirectoryNotEmpty
	}
	// Remove the directory
	root := path.Join(f.absRoot, dir)
	c, err := f.getSftpConnection(ctx)
	if err != nil {
		return fmt.Errorf("Rmdir: %w", err)
	}
	err = c.sftpClient.RemoveDirectory(root)
	f.putSftpConnection(&c, err)
	return err
}

// Move renames a remote sftp file object
func (f *Fs) Move(ctx context.Context, src fs.Object, remote string) (fs.Object, error) {
	srcObj, ok := src.(*Object)
	if !ok {
		fs.Debugf(src, "Can't move - not same remote type")
		return nil, fs.ErrorCantMove
	}
	err := f.mkParentDir(ctx, remote)
	if err != nil {
		return nil, fmt.Errorf("Move mkParentDir failed: %w", err)
	}
	c, err := f.getSftpConnection(ctx)
	if err != nil {
		return nil, fmt.Errorf("Move: %w", err)
	}
	err = c.sftpClient.Rename(
		srcObj.path(),
		path.Join(f.absRoot, remote),
	)
	f.putSftpConnection(&c, err)
	if err != nil {
		return nil, fmt.Errorf("Move Rename failed: %w", err)
	}
	dstObj, err := f.NewObject(ctx, remote)
	if err != nil {
		return nil, fmt.Errorf("Move NewObject failed: %w", err)
	}
	return dstObj, nil
}

// DirMove moves src, srcRemote to this remote at dstRemote
// using server-side move operations.
//
// Will only be called if src.Fs().Name() == f.Name()
//
// If it isn't possible then return fs.ErrorCantDirMove
//
// If destination exists then return fs.ErrorDirExists
func (f *Fs) DirMove(ctx context.Context, src fs.Fs, srcRemote, dstRemote string) error {
	srcFs, ok := src.(*Fs)
	if !ok {
		fs.Debugf(srcFs, "Can't move directory - not same remote type")
		return fs.ErrorCantDirMove
	}
	srcPath := path.Join(srcFs.absRoot, srcRemote)
	dstPath := path.Join(f.absRoot, dstRemote)

	// Check if destination exists
	ok, err := f.dirExists(ctx, dstPath)
	if err != nil {
		return fmt.Errorf("DirMove dirExists dst failed: %w", err)
	}
	if ok {
		return fs.ErrorDirExists
	}

	// Make sure the parent directory exists
	err = f.mkdir(ctx, path.Dir(dstPath))
	if err != nil {
		return fmt.Errorf("DirMove mkParentDir dst failed: %w", err)
	}

	// Do the move
	c, err := f.getSftpConnection(ctx)
	if err != nil {
		return fmt.Errorf("DirMove: %w", err)
	}
	err = c.sftpClient.Rename(
		srcPath,
		dstPath,
	)
	f.putSftpConnection(&c, err)
	if err != nil {
		return fmt.Errorf("DirMove Rename(%q,%q) failed: %w", srcPath, dstPath, err)
	}
	return nil
}

// run runds cmd on the remote end returning standard output
func (f *Fs) run(ctx context.Context, cmd string) ([]byte, error) {
	f.addSession() // Show session in use
	defer f.removeSession()

	c, err := f.getSftpConnection(ctx)
	if err != nil {
		return nil, fmt.Errorf("run: get SFTP connection: %w", err)
	}
	defer f.putSftpConnection(&c, err)

	session, err := c.sshClient.NewSession()
	if err != nil {
		return nil, fmt.Errorf("run: get SFTP session: %w", err)
	}
	defer func() {
		_ = session.Close()
	}()

	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr

	err = session.Run(cmd)
	if err != nil {
		return nil, fmt.Errorf("failed to run %q: %s: %w", cmd, stderr.Bytes(), err)
	}

	return stdout.Bytes(), nil
}

// Hashes returns the supported hash types of the filesystem
func (f *Fs) Hashes() hash.Set {
	ctx := context.TODO()
	if f.opt.DisableHashCheck {
		return hash.Set(hash.None)
	}

	if f.cachedHashes != nil {
		return *f.cachedHashes
	}

	// look for a hash command which works
	checkHash := func(commands []string, expected string, hashCommand *string, changed *bool) bool {
		if *hashCommand == hashCommandNotSupported {
			return false
		}
		if *hashCommand != "" {
			return true
		}
		*changed = true
		for _, command := range commands {
			output, err := f.run(ctx, command)
			if err != nil {
				continue
			}
			output = bytes.TrimSpace(output)
			fs.Debugf(f, "checking %q command: %q", command, output)
			if parseHash(output) == expected {
				*hashCommand = command
				return true
			}
		}
		*hashCommand = hashCommandNotSupported
		return false
	}

	changed := false
	md5Works := checkHash([]string{"md5sum", "md5 -r", "rclone md5sum"}, "d41d8cd98f00b204e9800998ecf8427e", &f.opt.Md5sumCommand, &changed)
	sha1Works := checkHash([]string{"sha1sum", "sha1 -r", "rclone sha1sum"}, "da39a3ee5e6b4b0d3255bfef95601890afd80709", &f.opt.Sha1sumCommand, &changed)

	if changed {
		f.m.Set("md5sum_command", f.opt.Md5sumCommand)
		f.m.Set("sha1sum_command", f.opt.Sha1sumCommand)
	}

	set := hash.NewHashSet()
	if sha1Works {
		set.Add(hash.SHA1)
	}
	if md5Works {
		set.Add(hash.MD5)
	}

	f.cachedHashes = &set
	return set
}

// About gets usage stats
func (f *Fs) About(ctx context.Context) (*fs.Usage, error) {
	escapedPath := shellEscape(f.root)
	if f.opt.PathOverride != "" {
		escapedPath = shellEscape(path.Join(f.opt.PathOverride, f.root))
	}
	if len(escapedPath) == 0 {
		escapedPath = "/"
	}
	stdout, err := f.run(ctx, "df -k "+escapedPath)
	if err != nil {
		return nil, fmt.Errorf("your remote may not support About: %w", err)
	}

	usageTotal, usageUsed, usageAvail := parseUsage(stdout)
	usage := &fs.Usage{}
	if usageTotal >= 0 {
		usage.Total = fs.NewUsageValue(usageTotal)
	}
	if usageUsed >= 0 {
		usage.Used = fs.NewUsageValue(usageUsed)
	}
	if usageAvail >= 0 {
		usage.Free = fs.NewUsageValue(usageAvail)
	}
	return usage, nil
}

// Shutdown the backend, closing any background tasks and any
// cached connections.
func (f *Fs) Shutdown(ctx context.Context) error {
	return f.drainPool(ctx)
}

// Fs is the filesystem this remote sftp file object is located within
func (o *Object) Fs() fs.Info {
	return o.fs
}

// String returns the URL to the remote SFTP file
func (o *Object) String() string {
	if o == nil {
		return "<nil>"
	}
	return o.remote
}

// Remote the name of the remote SFTP file, relative to the fs root
func (o *Object) Remote() string {
	return o.remote
}

// Hash returns the selected checksum of the file
// If no checksum is available it returns ""
func (o *Object) Hash(ctx context.Context, r hash.Type) (string, error) {
	o.fs.addSession() // Show session in use
	defer o.fs.removeSession()
	if o.fs.opt.DisableHashCheck {
		return "", nil
	}
	_ = o.fs.Hashes()

	var hashCmd string
	if r == hash.MD5 {
		if o.md5sum != nil {
			return *o.md5sum, nil
		}
		hashCmd = o.fs.opt.Md5sumCommand
	} else if r == hash.SHA1 {
		if o.sha1sum != nil {
			return *o.sha1sum, nil
		}
		hashCmd = o.fs.opt.Sha1sumCommand
	} else {
		return "", hash.ErrUnsupported
	}
	if hashCmd == "" || hashCmd == hashCommandNotSupported {
		return "", hash.ErrUnsupported
	}

	c, err := o.fs.getSftpConnection(ctx)
	if err != nil {
		return "", fmt.Errorf("Hash get SFTP connection: %w", err)
	}
	session, err := c.sshClient.NewSession()
	o.fs.putSftpConnection(&c, err)
	if err != nil {
		return "", fmt.Errorf("Hash put SFTP connection: %w", err)
	}

	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr
	escapedPath := shellEscape(o.path())
	if o.fs.opt.PathOverride != "" {
		escapedPath = shellEscape(path.Join(o.fs.opt.PathOverride, o.remote))
	}
	err = session.Run(hashCmd + " " + escapedPath)
	fs.Debugf(nil, "sftp cmd = %s", escapedPath)
	if err != nil {
		_ = session.Close()
		fs.Debugf(o, "Failed to calculate %v hash: %v (%s)", r, err, bytes.TrimSpace(stderr.Bytes()))
		return "", nil
	}

	_ = session.Close()
	b := stdout.Bytes()
	fs.Debugf(nil, "sftp output = %q", b)
	str := parseHash(b)
	fs.Debugf(nil, "sftp hash = %q", str)
	if r == hash.MD5 {
		o.md5sum = &str
	} else if r == hash.SHA1 {
		o.sha1sum = &str
	}
	return str, nil
}

var shellEscapeRegex = regexp.MustCompile("[^A-Za-z0-9_.,:/\\@\u0080-\uFFFFFFFF\n-]")

// Escape a string s.t. it cannot cause unintended behavior
// when sending it to a shell.
func shellEscape(str string) string {
	safe := shellEscapeRegex.ReplaceAllString(str, `\$0`)
	return strings.Replace(safe, "\n", "'\n'", -1)
}

// Converts a byte array from the SSH session returned by
// an invocation of md5sum/sha1sum to a hash string
// as expected by the rest of this application
func parseHash(bytes []byte) string {
	// For strings with backslash *sum writes a leading \
	// https://unix.stackexchange.com/q/313733/94054
	return strings.ToLower(strings.Split(strings.TrimLeft(string(bytes), "\\"), " ")[0]) // Split at hash / filename separator / all convert to lowercase
}

// Parses the byte array output from the SSH session
// returned by an invocation of df into
// the disk size, used space, and available space on the disk, in that order.
// Only works when `df` has output info on only one disk
func parseUsage(bytes []byte) (spaceTotal int64, spaceUsed int64, spaceAvail int64) {
	spaceTotal, spaceUsed, spaceAvail = -1, -1, -1
	lines := strings.Split(string(bytes), "\n")
	if len(lines) < 2 {
		return
	}
	split := strings.Fields(lines[1])
	if len(split) < 6 {
		return
	}
	spaceTotal, err := strconv.ParseInt(split[1], 10, 64)
	if err != nil {
		spaceTotal = -1
	}
	spaceUsed, err = strconv.ParseInt(split[2], 10, 64)
	if err != nil {
		spaceUsed = -1
	}
	spaceAvail, err = strconv.ParseInt(split[3], 10, 64)
	if err != nil {
		spaceAvail = -1
	}
	return spaceTotal * 1024, spaceUsed * 1024, spaceAvail * 1024
}

// Size returns the size in bytes of the remote sftp file
func (o *Object) Size() int64 {
	return o.size
}

// ModTime returns the modification time of the remote sftp file
func (o *Object) ModTime(ctx context.Context) time.Time {
	return o.modTime
}

// path returns the native path of the object
func (o *Object) path() string {
	return path.Join(o.fs.absRoot, o.remote)
}

// setMetadata updates the info in the object from the stat result passed in
func (o *Object) setMetadata(info os.FileInfo) {
	o.modTime = info.ModTime()
	o.size = info.Size()
	o.mode = info.Mode()
}

// statRemote stats the file or directory at the remote given
func (f *Fs) stat(ctx context.Context, remote string) (info os.FileInfo, err error) {
	c, err := f.getSftpConnection(ctx)
	if err != nil {
		return nil, fmt.Errorf("stat: %w", err)
	}
	absPath := path.Join(f.absRoot, remote)
	info, err = c.sftpClient.Stat(absPath)
	f.putSftpConnection(&c, err)
	return info, err
}

// stat updates the info in the Object
func (o *Object) stat(ctx context.Context) error {
	info, err := o.fs.stat(ctx, o.remote)
	if err != nil {
		if os.IsNotExist(err) {
			return fs.ErrorObjectNotFound
		}
		return fmt.Errorf("stat failed: %w", err)
	}
	if info.IsDir() {
		return fs.ErrorIsDir
	}
	o.setMetadata(info)
	return nil
}

// SetModTime sets the modification and access time to the specified time
//
// it also updates the info field
func (o *Object) SetModTime(ctx context.Context, modTime time.Time) error {
	if !o.fs.opt.SetModTime {
		return nil
	}
	c, err := o.fs.getSftpConnection(ctx)
	if err != nil {
		return fmt.Errorf("SetModTime: %w", err)
	}
	err = c.sftpClient.Chtimes(o.path(), modTime, modTime)
	o.fs.putSftpConnection(&c, err)
	if err != nil {
		return fmt.Errorf("SetModTime failed: %w", err)
	}
	err = o.stat(ctx)
	if err != nil {
		return fmt.Errorf("SetModTime stat failed: %w", err)
	}
	return nil
}

// Storable returns whether the remote sftp file is a regular file (not a directory, symbolic link, block device, character device, named pipe, etc.)
func (o *Object) Storable() bool {
	return o.mode.IsRegular()
}

// objectReader represents a file open for reading on the SFTP server
type objectReader struct {
	f          *Fs
	sftpFile   *sftp.File
	pipeReader *io.PipeReader
	done       chan struct{}
}

func (f *Fs) newObjectReader(sftpFile *sftp.File) *objectReader {
	pipeReader, pipeWriter := io.Pipe()
	file := &objectReader{
		f:          f,
		sftpFile:   sftpFile,
		pipeReader: pipeReader,
		done:       make(chan struct{}),
	}
	// Show connection in use
	f.addSession()

	go func() {
		// Use sftpFile.WriteTo to pump data so that it gets a
		// chance to build the window up.
		_, err := sftpFile.WriteTo(pipeWriter)
		// Close the pipeWriter so the pipeReader fails with
		// the same error or EOF if err == nil
		_ = pipeWriter.CloseWithError(err)
		// signal that we've finished
		close(file.done)
	}()

	return file
}

// Read from a remote sftp file object reader
func (file *objectReader) Read(p []byte) (n int, err error) {
	n, err = file.pipeReader.Read(p)
	return n, err
}

// Close a reader of a remote sftp file
func (file *objectReader) Close() (err error) {
	// Close the sftpFile - this will likely cause the WriteTo to error
	err = file.sftpFile.Close()
	// Close the pipeReader so writes to the pipeWriter fail
	_ = file.pipeReader.Close()
	// Wait for the background process to finish
	<-file.done
	// Show connection no longer in use
	file.f.removeSession()
	return err
}

// Open a remote sftp file object for reading. Seek is supported
func (o *Object) Open(ctx context.Context, options ...fs.OpenOption) (in io.ReadCloser, err error) {
	var offset, limit int64 = 0, -1
	for _, option := range options {
		switch x := option.(type) {
		case *fs.SeekOption:
			offset = x.Offset
		case *fs.RangeOption:
			offset, limit = x.Decode(o.Size())
		default:
			if option.Mandatory() {
				fs.Logf(o, "Unsupported mandatory option: %v", option)
			}
		}
	}
	c, err := o.fs.getSftpConnection(ctx)
	if err != nil {
		return nil, fmt.Errorf("Open: %w", err)
	}
	sftpFile, err := c.sftpClient.Open(o.path())
	o.fs.putSftpConnection(&c, err)
	if err != nil {
		return nil, fmt.Errorf("Open failed: %w", err)
	}
	if offset > 0 {
		off, err := sftpFile.Seek(offset, io.SeekStart)
		if err != nil || off != offset {
			return nil, fmt.Errorf("Open Seek failed: %w", err)
		}
	}
	in = readers.NewLimitedReadCloser(o.fs.newObjectReader(sftpFile), limit)
	return in, nil
}

type sizeReader struct {
	io.Reader
	size int64
}

// Size returns the expected size of the stream
//
// It is used in sftpFile.ReadFrom as a hint to work out the
// concurrency needed
func (sr *sizeReader) Size() int64 {
	return sr.size
}

// Update a remote sftp file using the data <in> and ModTime from <src>
func (o *Object) Update(ctx context.Context, in io.Reader, src fs.ObjectInfo, options ...fs.OpenOption) error {
	o.fs.addSession() // Show session in use
	defer o.fs.removeSession()
	// Clear the hash cache since we are about to update the object
	o.md5sum = nil
	o.sha1sum = nil
	c, err := o.fs.getSftpConnection(ctx)
	if err != nil {
		return fmt.Errorf("Update: %w", err)
	}
	file, err := c.sftpClient.OpenFile(o.path(), os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	o.fs.putSftpConnection(&c, err)
	if err != nil {
		return fmt.Errorf("Update Create failed: %w", err)
	}
	// remove the file if upload failed
	remove := func() {
		c, removeErr := o.fs.getSftpConnection(ctx)
		if removeErr != nil {
			fs.Debugf(src, "Failed to open new SSH connection for delete: %v", removeErr)
			return
		}
		removeErr = c.sftpClient.Remove(o.path())
		o.fs.putSftpConnection(&c, removeErr)
		if removeErr != nil {
			fs.Debugf(src, "Failed to remove: %v", removeErr)
		} else {
			fs.Debugf(src, "Removed after failed upload: %v", err)
		}
	}
	_, err = file.ReadFrom(&sizeReader{Reader: in, size: src.Size()})
	if err != nil {
		remove()
		return fmt.Errorf("Update ReadFrom failed: %w", err)
	}
	err = file.Close()
	if err != nil {
		remove()
		return fmt.Errorf("Update Close failed: %w", err)
	}

	// Set the mod time - this stats the object if o.fs.opt.SetModTime == true
	err = o.SetModTime(ctx, src.ModTime(ctx))
	if err != nil {
		return fmt.Errorf("Update SetModTime failed: %w", err)
	}

	// Stat the file after the upload to read its stats back if o.fs.opt.SetModTime == false
	if !o.fs.opt.SetModTime {
		err = o.stat(ctx)
		if err == fs.ErrorObjectNotFound {
			// In the specific case of o.fs.opt.SetModTime == false
			// if the object wasn't found then don't return an error
			fs.Debugf(o, "Not found after upload with set_modtime=false so returning best guess")
			o.modTime = src.ModTime(ctx)
			o.size = src.Size()
			o.mode = os.FileMode(0666) // regular file
		} else if err != nil {
			return fmt.Errorf("Update stat failed: %w", err)
		}
	}

	return nil
}

// Remove a remote sftp file object
func (o *Object) Remove(ctx context.Context) error {
	c, err := o.fs.getSftpConnection(ctx)
	if err != nil {
		return fmt.Errorf("Remove: %w", err)
	}
	err = c.sftpClient.Remove(o.path())
	o.fs.putSftpConnection(&c, err)
	return err
}

// Check the interfaces are satisfied
var (
	_ fs.Fs          = &Fs{}
	_ fs.PutStreamer = &Fs{}
	_ fs.Mover       = &Fs{}
	_ fs.DirMover    = &Fs{}
	_ fs.Abouter     = &Fs{}
	_ fs.Shutdowner  = &Fs{}
	_ fs.Object      = &Object{}
)
