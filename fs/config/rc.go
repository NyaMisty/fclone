package config

import (
	"context"
	"errors"

	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/rc"
)

func init() {
	rc.Add(rc.Call{
		Path:         "config/dump",
		Fn:           rcDump,
		Title:        "Dumps the config file.",
		AuthRequired: true,
		Help: `
Returns a JSON object:
- key: value

Where keys are remote names and values are the config parameters.

See the [config dump command](/commands/rclone_config_dump/) command for more information on the above.
`,
	})
}

// Return the config file dump
func rcDump(ctx context.Context, in rc.Params) (out rc.Params, err error) {
	return DumpRcBlob(), nil
}

func init() {
	rc.Add(rc.Call{
		Path:         "config/get",
		Fn:           rcGet,
		Title:        "Get a remote in the config file.",
		AuthRequired: true,
		Help: `
Parameters:

- name - name of remote to get

See the [config dump command](/commands/rclone_config_dump/) command for more information on the above.
`,
	})
}

// Return the config file get
func rcGet(ctx context.Context, in rc.Params) (out rc.Params, err error) {
	name, err := in.GetString("name")
	if err != nil {
		return nil, err
	}
	return DumpRcRemote(name), nil
}

func init() {
	rc.Add(rc.Call{
		Path:         "config/listremotes",
		Fn:           rcListRemotes,
		Title:        "Lists the remotes in the config file.",
		AuthRequired: true,
		Help: `
Returns
- remotes - array of remote names

See the [listremotes command](/commands/rclone_listremotes/) command for more information on the above.
`,
	})
}

// Return the a list of remotes in the config file
func rcListRemotes(ctx context.Context, in rc.Params) (out rc.Params, err error) {
	var remotes = []string{}
	for _, remote := range LoadedData().GetSectionList() {
		remotes = append(remotes, remote)
	}
	out = rc.Params{
		"remotes": remotes,
	}
	return out, nil
}

func init() {
	rc.Add(rc.Call{
		Path:         "config/providers",
		Fn:           rcProviders,
		Title:        "Shows how providers are configured in the config file.",
		AuthRequired: true,
		Help: `
Returns a JSON object:
- providers - array of objects

See the [config providers command](/commands/rclone_config_providers/) command for more information on the above.
`,
	})
}

// Return the config file providers
func rcProviders(ctx context.Context, in rc.Params) (out rc.Params, err error) {
	out = rc.Params{
		"providers": fs.Registry,
	}
	return out, nil
}

func init() {
	for _, name := range []string{"create", "update", "password"} {
		name := name
		extraHelp := ""
		if name == "create" {
			extraHelp = "- type - type of the new remote\n"
		}
		if name == "create" || name == "update" {
			extraHelp += `- opt - a dictionary of options to control the configuration
    - obscure - declare passwords are plain and need obscuring
    - noObscure - declare passwords are already obscured and don't need obscuring
    - nonInteractive - don't interact with a user, return questions
    - continue - continue the config process with an answer
    - all - ask all the config questions not just the post config ones
    - state - state to restart with - used with continue
    - result - result to restart with - used with continue
`
		}
		rc.Add(rc.Call{
			Path:         "config/" + name,
			AuthRequired: true,
			Fn: func(ctx context.Context, in rc.Params) (rc.Params, error) {
				return rcConfig(ctx, in, name)
			},
			Title: name + " the config for a remote.",
			Help: `This takes the following parameters:

- name - name of remote
- parameters - a map of \{ "key": "value" \} pairs
` + extraHelp + `

See the [config ` + name + ` command](/commands/rclone_config_` + name + `/) command for more information on the above.`,
		})
	}
}

// Manipulate the config file
func rcConfig(ctx context.Context, in rc.Params, what string) (out rc.Params, err error) {
	name, err := in.GetString("name")
	if err != nil {
		return nil, err
	}
	parameters := rc.Params{}
	err = in.GetStruct("parameters", &parameters)
	if err != nil {
		return nil, err
	}
	var opt UpdateRemoteOpt
	err = in.GetStruct("opt", &opt)
	if err != nil && !rc.IsErrParamNotFound(err) {
		return nil, err
	}
	// Backwards compatibility
	if value, err := in.GetBool("obscure"); err == nil {
		opt.Obscure = value
	}
	if value, err := in.GetBool("noObscure"); err == nil {
		opt.NoObscure = value
	}
	var configOut *fs.ConfigOut
	switch what {
	case "create":
		remoteType, typeErr := in.GetString("type")
		if typeErr != nil {
			return nil, typeErr
		}
		configOut, err = CreateRemote(ctx, name, remoteType, parameters, opt)
	case "update":
		configOut, err = UpdateRemote(ctx, name, parameters, opt)
	case "password":
		err = PasswordRemote(ctx, name, parameters)
	default:
		err = errors.New("unknown rcConfig type")
	}
	if err != nil {
		return nil, err
	}
	if !opt.NonInteractive {
		return nil, nil
	}
	if configOut == nil {
		configOut = &fs.ConfigOut{}
	}
	err = rc.Reshape(&out, configOut)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func init() {
	rc.Add(rc.Call{
		Path:         "config/delete",
		Fn:           rcDelete,
		Title:        "Delete a remote in the config file.",
		AuthRequired: true,
		Help: `
Parameters:

- name - name of remote to delete

See the [config delete command](/commands/rclone_config_delete/) command for more information on the above.
`,
	})
}

// Return the config file delete
func rcDelete(ctx context.Context, in rc.Params) (out rc.Params, err error) {
	name, err := in.GetString("name")
	if err != nil {
		return nil, err
	}
	DeleteRemote(name)
	return nil, nil
}
