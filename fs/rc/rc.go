// Package rc implements a remote control server and registry for rclone
//
// To register your internal calls, call rc.Add(path, function).  Your
// function should take ane return a Param.  It can also return an
// error.  Use rc.NewError to wrap an existing error along with an
// http response type if another response other than 500 internal
// error is required on error.
package rc

import (
	"encoding/json"
	"io"
	_ "net/http/pprof" // install the pprof http handlers
	"time"

	libhttp "github.com/rclone/rclone/lib/http"
)

// Options contains options for the remote control server
type Options struct {
	HTTP                     libhttp.Config
	Auth                     libhttp.AuthConfig
	Template                 libhttp.TemplateConfig
	Enabled                  bool   // set to enable the server
	Serve                    bool   // set to serve files from remotes
	Files                    string // set to enable serving files locally
	NoAuth                   bool   // set to disable auth checks on AuthRequired methods
	WebUI                    bool   // set to launch the web ui
	WebGUIUpdate             bool   // set to check new update
	WebGUIForceUpdate        bool   // set to force download new update
	WebGUINoOpenBrowser      bool   // set to disable auto opening browser
	WebGUIFetchURL           string // set the default url for fetching webgui
	AccessControlAllowOrigin string // set the access control for CORS configuration
	EnableMetrics            bool   // set to disable prometheus metrics on /metrics
	JobExpireDuration        time.Duration
	JobExpireInterval        time.Duration
}

// DefaultOpt is the default values used for Options
var DefaultOpt = Options{
	HTTP:              libhttp.DefaultCfg(),
	Auth:              libhttp.DefaultAuthCfg(),
	Template:          libhttp.DefaultTemplateCfg(),
	Enabled:           false,
	JobExpireDuration: 60 * time.Second,
	JobExpireInterval: 10 * time.Second,
}

func init() {
	DefaultOpt.HTTP.ListenAddr = []string{"localhost:5572"}
}

// WriteJSON writes JSON in out to w
func WriteJSON(w io.Writer, out Params) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "\t")
	return enc.Encode(out)
}
