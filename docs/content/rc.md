---
title: "Remote Control / API"
description: "Remote controlling rclone with its API"
---

# Remote controlling rclone with its API

If rclone is run with the `--rc` flag then it starts an HTTP server
which can be used to remote control rclone using its API.

You can either use the [rclone rc](#api-rc) command to access the API
or [use HTTP directly](#api-http).

If you just want to run a remote control then see the [rcd command](/commands/rclone_rcd/).

## Supported parameters

### --rc

Flag to start the http server listen on remote requests
      
### --rc-addr=IP

IPaddress:Port or :Port to bind server to. (default "localhost:5572")

### --rc-cert=KEY
SSL PEM key (concatenation of certificate and CA certificate)

### --rc-client-ca=PATH
Client certificate authority to verify clients with

### --rc-htpasswd=PATH

htpasswd file - if not provided no authentication is done

### --rc-key=PATH

SSL PEM Private key

### --rc-max-header-bytes=VALUE

Maximum size of request header (default 4096)

### --rc-user=VALUE

User name for authentication.

### --rc-pass=VALUE

Password for authentication.

### --rc-realm=VALUE

Realm for authentication (default "rclone")

### --rc-server-read-timeout=DURATION

Timeout for server reading data (default 1h0m0s)

### --rc-server-write-timeout=DURATION

Timeout for server writing data (default 1h0m0s)

### --rc-serve

Enable the serving of remote objects via the HTTP interface.  This
means objects will be accessible at http://127.0.0.1:5572/ by default,
so you can browse to http://127.0.0.1:5572/ or http://127.0.0.1:5572/*
to see a listing of the remotes.  Objects may be requested from
remotes using this syntax http://127.0.0.1:5572/[remote:path]/path/to/object

Default Off.

### --rc-files /path/to/directory

Path to local files to serve on the HTTP server.

If this is set then rclone will serve the files in that directory.  It
will also open the root in the web browser if specified.  This is for
implementing browser based GUIs for rclone functions.

If `--rc-user` or `--rc-pass` is set then the URL that is opened will
have the authorization in the URL in the `http://user:pass@localhost/`
style.

Default Off.

### --rc-enable-metrics

Enable OpenMetrics/Prometheus compatible endpoint at `/metrics`.

Default Off.

### --rc-web-gui

Set this flag to serve the default web gui on the same port as rclone.

Default Off.

### --rc-allow-origin

Set the allowed Access-Control-Allow-Origin for rc requests.

Can be used with --rc-web-gui if the rclone is running on different IP than the web-gui.

Default is IP address on which rc is running.

### --rc-web-fetch-url

Set the URL to fetch the rclone-web-gui files from.

Default https://api.github.com/repos/rclone/rclone-webui-react/releases/latest.

### --rc-web-gui-update

Set this flag to check and update rclone-webui-react from the rc-web-fetch-url.

Default Off.

### --rc-web-gui-force-update

Set this flag to force update rclone-webui-react from the rc-web-fetch-url.

Default Off.

### --rc-web-gui-no-open-browser

Set this flag to disable opening browser automatically when using web-gui.

Default Off.

### --rc-job-expire-duration=DURATION

Expire finished async jobs older than DURATION (default 60s).

### --rc-job-expire-interval=DURATION

Interval duration to check for expired async jobs (default 10s).

### --rc-no-auth

By default rclone will require authorisation to have been set up on
the rc interface in order to use any methods which access any rclone
remotes.  Eg `operations/list` is denied as it involved creating a
remote as is `sync/copy`.

If this is set then no authorisation will be required on the server to
use these methods.  The alternative is to use `--rc-user` and
`--rc-pass` and use these credentials in the request.

Default Off.

## Accessing the remote control via the rclone rc command {#api-rc}

Rclone itself implements the remote control protocol in its `rclone
rc` command.

You can use it like this

```
$ rclone rc rc/noop param1=one param2=two
{
	"param1": "one",
	"param2": "two"
}
```

Run `rclone rc` on its own to see the help for the installed remote
control commands.

## JSON input

`rclone rc` also supports a `--json` flag which can be used to send
more complicated input parameters.

```
$ rclone rc --json '{ "p1": [1,"2",null,4], "p2": { "a":1, "b":2 } }' rc/noop
{
	"p1": [
		1,
		"2",
		null,
		4
	],
	"p2": {
		"a": 1,
		"b": 2
	}
}
```

If the parameter being passed is an object then it can be passed as a
JSON string rather than using the `--json` flag which simplifies the
command line.

```
rclone rc operations/list fs=/tmp remote=test opt='{"showHash": true}'
```

Rather than

```
rclone rc operations/list --json '{"fs": "/tmp", "remote": "test", "opt": {"showHash": true}}'
```

## Special parameters

The rc interface supports some special parameters which apply to
**all** commands.  These start with `_` to show they are different.

### Running asynchronous jobs with _async = true

Each rc call is classified as a job and it is assigned its own id. By default
jobs are executed immediately as they are created or synchronously.

If `_async` has a true value when supplied to an rc call then it will
return immediately with a job id and the task will be run in the
background.  The `job/status` call can be used to get information of
the background job.  The job can be queried for up to 1 minute after
it has finished.

It is recommended that potentially long running jobs, e.g. `sync/sync`,
`sync/copy`, `sync/move`, `operations/purge` are run with the `_async`
flag to avoid any potential problems with the HTTP request and
response timing out.

Starting a job with the `_async` flag:

```
$ rclone rc --json '{ "p1": [1,"2",null,4], "p2": { "a":1, "b":2 }, "_async": true }' rc/noop
{
	"jobid": 2
}
```

Query the status to see if the job has finished.  For more information
on the meaning of these return parameters see the `job/status` call.

```
$ rclone rc --json '{ "jobid":2 }' job/status
{
	"duration": 0.000124163,
	"endTime": "2018-10-27T11:38:07.911245881+01:00",
	"error": "",
	"finished": true,
	"id": 2,
	"output": {
		"_async": true,
		"p1": [
			1,
			"2",
			null,
			4
		],
		"p2": {
			"a": 1,
			"b": 2
		}
	},
	"startTime": "2018-10-27T11:38:07.911121728+01:00",
	"success": true
}
```

`job/list` can be used to show the running or recently completed jobs

```
$ rclone rc job/list
{
	"jobids": [
		2
	]
}
```

### Setting config flags with _config

If you wish to set config (the equivalent of the global flags) for the
duration of an rc call only then pass in the `_config` parameter.

This should be in the same format as the `config` key returned by
[options/get](#options-get).

For example, if you wished to run a sync with the `--checksum`
parameter, you would pass this parameter in your JSON blob.

    "_config":{"CheckSum": true}

If using `rclone rc` this could be passed as

    rclone rc operations/sync ... _config='{"CheckSum": true}'

Any config parameters you don't set will inherit the global defaults
which were set with command line flags or environment variables.

Note that it is possible to set some values as strings or integers -
see [data types](/#data-types) for more info. Here is an example
setting the equivalent of `--buffer-size` in string or integer format.

    "_config":{"BufferSize": "42M"}
    "_config":{"BufferSize": 44040192}

If you wish to check the `_config` assignment has worked properly then
calling `options/local` will show what the value got set to.

### Setting filter flags with _filter

If you wish to set filters for the duration of an rc call only then
pass in the `_filter` parameter.

This should be in the same format as the `filter` key returned by
[options/get](#options-get).

For example, if you wished to run a sync with these flags

    --max-size 1M --max-age 42s --include "a" --include "b"

you would pass this parameter in your JSON blob.

    "_filter":{"MaxSize":"1M", "IncludeRule":["a","b"], "MaxAge":"42s"}

If using `rclone rc` this could be passed as

    rclone rc ... _filter='{"MaxSize":"1M", "IncludeRule":["a","b"], "MaxAge":"42s"}'

Any filter parameters you don't set will inherit the global defaults
which were set with command line flags or environment variables.

Note that it is possible to set some values as strings or integers -
see [data types](/#data-types) for more info. Here is an example
setting the equivalent of `--buffer-size` in string or integer format.

    "_filter":{"MinSize": "42M"}
    "_filter":{"MinSize": 44040192}

If you wish to check the `_filter` assignment has worked properly then
calling `options/local` will show what the value got set to.

### Assigning operations to groups with _group = value

Each rc call has its own stats group for tracking its metrics. By default
grouping is done by the composite group name from prefix `job/` and  id of the
job like so `job/1`.

If `_group` has a value then stats for that request will be grouped under that
value. This allows caller to group stats under their own name.

Stats for specific group can be accessed by passing `group` to `core/stats`:

```
$ rclone rc --json '{ "group": "job/1" }' core/stats
{
	"speed": 12345
	...
}
```

## Data types {#data-types}

When the API returns types, these will mostly be straight forward
integer, string or boolean types.

However some of the types returned by the [options/get](#options-get)
call and taken by the [options/set](#options-set) calls as well as the
`vfsOpt`, `mountOpt` and the `_config` parameters.

- `Duration` - these are returned as an integer duration in
  nanoseconds. They may be set as an integer, or they may be set with
  time string, eg "5s". See the [options section](/docs/#options) for
  more info.
- `Size` - these are returned as an integer number of bytes. They may
  be set as an integer or they may be set with a size suffix string,
  eg "10M". See the [options section](/docs/#options) for more info.
- Enumerated type (such as `CutoffMode`, `DumpFlags`, `LogLevel`,
  `VfsCacheMode` - these will be returned as an integer and may be set
  as an integer but more conveniently they can be set as a string, eg
  "HARD" for `CutoffMode` or `DEBUG` for `LogLevel`.
- `BandwidthSpec` - this will be set and returned as a string, eg
  "1M".

## Specifying remotes to work on

Remotes are specified with the `fs=`, `srcFs=`, `dstFs=`
parameters depending on the command being used.

The parameters can be a string as per the rest of rclone, eg
`s3:bucket/path` or `:sftp:/my/dir`. They can also be specified as
JSON blobs.

If specifyng a JSON blob it should be a object mapping strings to
strings. These values will be used to configure the remote. There are
3 special values which may be set:

- `type` -  set to `type` to specify a remote called `:type:`
- `_name` - set to `name` to specify a remote called `name:`
- `_root` - sets the root of the remote - may be empty

One of `_name` or `type` should normally be set. If the `local`
backend is desired then `type` should be set to `local`. If `_root`
isn't specified then it defaults to the root of the remote.

For example this JSON is equivalent to `remote:/tmp`

```
{
    "_name": "remote",
    "_path": "/tmp"
}
```

And this is equivalent to `:sftp,host='example.com':/tmp`

```
{
    "type": "sftp",
    "host": "example.com",
    "_path": "/tmp"
}
```

And this is equivalent to `/tmp/dir`

```
{
    type = "local",
    _ path = "/tmp/dir"
}
```

## Supported commands
{{< rem autogenerated start "- run make rcdocs - don't edit here" >}}
### backend/command: Runs a backend command. {#backend-command}

This takes the following parameters:

- command - a string with the command name
- fs - a remote name string e.g. "drive:"
- arg - a list of arguments for the backend command
- opt - a map of string to string of options

Returns:

- result - result from the backend command

Example:

    rclone rc backend/command command=noop fs=. -o echo=yes -o blue -a path1 -a path2

Returns

```
{
	"result": {
		"arg": [
			"path1",
			"path2"
		],
		"name": "noop",
		"opt": {
			"blue": "",
			"echo": "yes"
		}
	}
}
```

Note that this is the direct equivalent of using this "backend"
command:

    rclone backend noop . -o echo=yes -o blue path1 path2

Note that arguments must be preceded by the "-a" flag

See the [backend](/commands/rclone_backend/) command for more information.

**Authentication is required for this call.**

### cache/expire: Purge a remote from cache {#cache-expire}

Purge a remote from the cache backend. Supports either a directory or a file.
Params:
  - remote = path to remote (required)
  - withData = true/false to delete cached data (chunks) as well (optional)

Eg

    rclone rc cache/expire remote=path/to/sub/folder/
    rclone rc cache/expire remote=/ withData=true

### cache/fetch: Fetch file chunks {#cache-fetch}

Ensure the specified file chunks are cached on disk.

The chunks= parameter specifies the file chunks to check.
It takes a comma separated list of array slice indices.
The slice indices are similar to Python slices: start[:end]

start is the 0 based chunk number from the beginning of the file
to fetch inclusive. end is 0 based chunk number from the beginning
of the file to fetch exclusive.
Both values can be negative, in which case they count from the back
of the file. The value "-5:" represents the last 5 chunks of a file.

Some valid examples are:
":5,-5:" -> the first and last five chunks
"0,-2" -> the first and the second last chunk
"0:10" -> the first ten chunks

Any parameter with a key that starts with "file" can be used to
specify files to fetch, e.g.

    rclone rc cache/fetch chunks=0 file=hello file2=home/goodbye

File names will automatically be encrypted when the a crypt remote
is used on top of the cache.

### cache/stats: Get cache stats {#cache-stats}

Show statistics for the cache remote.

### config/create: create the config for a remote. {#config-create}

This takes the following parameters:

- name - name of remote
- parameters - a map of \{ "key": "value" \} pairs
- type - type of the new remote
- opt - a dictionary of options to control the configuration
    - obscure - declare passwords are plain and need obscuring
    - noObscure - declare passwords are already obscured and don't need obscuring
    - nonInteractive - don't interact with a user, return questions
    - continue - continue the config process with an answer
    - all - ask all the config questions not just the post config ones
    - state - state to restart with - used with continue
    - result - result to restart with - used with continue


See the [config create command](/commands/rclone_config_create/) command for more information on the above.

**Authentication is required for this call.**

### config/delete: Delete a remote in the config file. {#config-delete}

Parameters:

- name - name of remote to delete

See the [config delete command](/commands/rclone_config_delete/) command for more information on the above.

**Authentication is required for this call.**

### config/dump: Dumps the config file. {#config-dump}

Returns a JSON object:
- key: value

Where keys are remote names and values are the config parameters.

See the [config dump command](/commands/rclone_config_dump/) command for more information on the above.

**Authentication is required for this call.**

### config/get: Get a remote in the config file. {#config-get}

Parameters:

- name - name of remote to get

See the [config dump command](/commands/rclone_config_dump/) command for more information on the above.

**Authentication is required for this call.**

### config/listremotes: Lists the remotes in the config file. {#config-listremotes}

Returns
- remotes - array of remote names

See the [listremotes command](/commands/rclone_listremotes/) command for more information on the above.

**Authentication is required for this call.**

### config/password: password the config for a remote. {#config-password}

This takes the following parameters:

- name - name of remote
- parameters - a map of \{ "key": "value" \} pairs


See the [config password command](/commands/rclone_config_password/) command for more information on the above.

**Authentication is required for this call.**

### config/providers: Shows how providers are configured in the config file. {#config-providers}

Returns a JSON object:
- providers - array of objects

See the [config providers command](/commands/rclone_config_providers/) command for more information on the above.

**Authentication is required for this call.**

### config/update: update the config for a remote. {#config-update}

This takes the following parameters:

- name - name of remote
- parameters - a map of \{ "key": "value" \} pairs
- opt - a dictionary of options to control the configuration
    - obscure - declare passwords are plain and need obscuring
    - noObscure - declare passwords are already obscured and don't need obscuring
    - nonInteractive - don't interact with a user, return questions
    - continue - continue the config process with an answer
    - all - ask all the config questions not just the post config ones
    - state - state to restart with - used with continue
    - result - result to restart with - used with continue


See the [config update command](/commands/rclone_config_update/) command for more information on the above.

**Authentication is required for this call.**

### core/bwlimit: Set the bandwidth limit. {#core-bwlimit}

This sets the bandwidth limit to the string passed in. This should be
a single bandwidth limit entry or a pair of upload:download bandwidth.

Eg

    rclone rc core/bwlimit rate=off
    {
        "bytesPerSecond": -1,
        "bytesPerSecondTx": -1,
        "bytesPerSecondRx": -1,
        "rate": "off"
    }
    rclone rc core/bwlimit rate=1M
    {
        "bytesPerSecond": 1048576,
        "bytesPerSecondTx": 1048576,
        "bytesPerSecondRx": 1048576,
        "rate": "1M"
    }
    rclone rc core/bwlimit rate=1M:100k
    {
        "bytesPerSecond": 1048576,
        "bytesPerSecondTx": 1048576,
        "bytesPerSecondRx": 131072,
        "rate": "1M"
    }


If the rate parameter is not supplied then the bandwidth is queried

    rclone rc core/bwlimit
    {
        "bytesPerSecond": 1048576,
        "bytesPerSecondTx": 1048576,
        "bytesPerSecondRx": 1048576,
        "rate": "1M"
    }

The format of the parameter is exactly the same as passed to --bwlimit
except only one bandwidth may be specified.

In either case "rate" is returned as a human readable string, and
"bytesPerSecond" is returned as a number.

### core/command: Run a rclone terminal command over rc. {#core-command}

This takes the following parameters:

- command - a string with the command name.
- arg - a list of arguments for the backend command.
- opt - a map of string to string of options.
- returnType - one of ("COMBINED_OUTPUT", "STREAM", "STREAM_ONLY_STDOUT", "STREAM_ONLY_STDERR").
    - Defaults to "COMBINED_OUTPUT" if not set.
    - The STREAM returnTypes will write the output to the body of the HTTP message.
    - The COMBINED_OUTPUT will write the output to the "result" parameter.

Returns:

- result - result from the backend command.
    - Only set when using returnType "COMBINED_OUTPUT".
- error	 - set if rclone exits with an error code.
- returnType - one of ("COMBINED_OUTPUT", "STREAM", "STREAM_ONLY_STDOUT", "STREAM_ONLY_STDERR").

Example:

    rclone rc core/command command=ls -a mydrive:/ -o max-depth=1
    rclone rc core/command -a ls -a mydrive:/ -o max-depth=1

Returns:

```
{
	"error": false,
	"result": "<Raw command line output>"
}

OR 
{
	"error": true,
	"result": "<Raw command line output>"
}

```

**Authentication is required for this call.**

### core/gc: Runs a garbage collection. {#core-gc}

This tells the go runtime to do a garbage collection run.  It isn't
necessary to call this normally, but it can be useful for debugging
memory problems.

### core/group-list: Returns list of stats. {#core-group-list}

This returns list of stats groups currently in memory. 

Returns the following values:
```
{
	"groups":  an array of group names:
		[
			"group1",
			"group2",
			...
		]
}
```

### core/memstats: Returns the memory statistics {#core-memstats}

This returns the memory statistics of the running program.  What the values mean
are explained in the go docs: https://golang.org/pkg/runtime/#MemStats

The most interesting values for most people are:

- HeapAlloc - this is the amount of memory rclone is actually using
- HeapSys - this is the amount of memory rclone has obtained from the OS
- Sys - this is the total amount of memory requested from the OS
   - It is virtual memory so may include unused memory

### core/obscure: Obscures a string passed in. {#core-obscure}

Pass a clear string and rclone will obscure it for the config file:
- clear - string

Returns:
- obscured - string

### core/pid: Return PID of current process {#core-pid}

This returns PID of current process.
Useful for stopping rclone process.

### core/quit: Terminates the app. {#core-quit}

(Optional) Pass an exit code to be used for terminating the app:
- exitCode - int

### core/stats: Returns stats about current transfers. {#core-stats}

This returns all available stats:

	rclone rc core/stats

If group is not provided then summed up stats for all groups will be
returned.

Parameters

- group - name of the stats group (string)

Returns the following values:

```
{
	"bytes": total transferred bytes since the start of the group,
	"checks": number of files checked,
	"deletes" : number of files deleted,
	"elapsedTime": time in floating point seconds since rclone was started,
	"errors": number of errors,
	"eta": estimated time in seconds until the group completes,
	"fatalError": boolean whether there has been at least one fatal error,
	"lastError": last error string,
	"renames" : number of files renamed,
	"retryError": boolean showing whether there has been at least one non-NoRetryError,
	"speed": average speed in bytes per second since start of the group,
	"totalBytes": total number of bytes in the group,
	"totalChecks": total number of checks in the group,
	"totalTransfers": total number of transfers in the group,
	"transferTime" : total time spent on running jobs,
	"transfers": number of transferred files,
	"transferring": an array of currently active file transfers:
		[
			{
				"bytes": total transferred bytes for this file,
				"eta": estimated time in seconds until file transfer completion
				"name": name of the file,
				"percentage": progress of the file transfer in percent,
				"speed": average speed over the whole transfer in bytes per second,
				"speedAvg": current speed in bytes per second as an exponentially weighted moving average,
				"size": size of the file in bytes
			}
		],
	"checking": an array of names of currently active file checks
		[]
}
```
Values for "transferring", "checking" and "lastError" are only assigned if data is available.
The value for "eta" is null if an eta cannot be determined.

### core/stats-delete: Delete stats group. {#core-stats-delete}

This deletes entire stats group.

Parameters

- group - name of the stats group (string)

### core/stats-reset: Reset stats. {#core-stats-reset}

This clears counters, errors and finished transfers for all stats or specific 
stats group if group is provided.

Parameters

- group - name of the stats group (string)

### core/transferred: Returns stats about completed transfers. {#core-transferred}

This returns stats about completed transfers:

	rclone rc core/transferred

If group is not provided then completed transfers for all groups will be
returned.

Note only the last 100 completed transfers are returned.

Parameters

- group - name of the stats group (string)

Returns the following values:
```
{
	"transferred":  an array of completed transfers (including failed ones):
		[
			{
				"name": name of the file,
				"size": size of the file in bytes,
				"bytes": total transferred bytes for this file,
				"checked": if the transfer is only checked (skipped, deleted),
				"timestamp": integer representing millisecond unix epoch,
				"error": string description of the error (empty if successful),
				"jobid": id of the job that this transfer belongs to
			}
		]
}
```

### core/version: Shows the current version of rclone and the go runtime. {#core-version}

This shows the current version of go and the go runtime:

- version - rclone version, e.g. "v1.53.0"
- decomposed - version number as [major, minor, patch]
- isGit - boolean - true if this was compiled from the git version
- isBeta - boolean - true if this is a beta version
- os - OS in use as according to Go
- arch - cpu architecture in use according to Go
- goVersion - version of Go runtime in use
- linking - type of rclone executable (static or dynamic)
- goTags - space separated build tags or "none"

### debug/set-block-profile-rate: Set runtime.SetBlockProfileRate for blocking profiling. {#debug-set-block-profile-rate}

SetBlockProfileRate controls the fraction of goroutine blocking events
that are reported in the blocking profile. The profiler aims to sample
an average of one blocking event per rate nanoseconds spent blocked.

To include every blocking event in the profile, pass rate = 1. To turn
off profiling entirely, pass rate <= 0.

After calling this you can use this to see the blocking profile:

    go tool pprof http://localhost:5572/debug/pprof/block

Parameters:

- rate - int

### debug/set-mutex-profile-fraction: Set runtime.SetMutexProfileFraction for mutex profiling. {#debug-set-mutex-profile-fraction}

SetMutexProfileFraction controls the fraction of mutex contention
events that are reported in the mutex profile. On average 1/rate
events are reported. The previous rate is returned.

To turn off profiling entirely, pass rate 0. To just read the current
rate, pass rate < 0. (For n>1 the details of sampling may change.)

Once this is set you can look use this to profile the mutex contention:

    go tool pprof http://localhost:5572/debug/pprof/mutex

Parameters:

- rate - int

Results:

- previousRate - int

### fscache/clear: Clear the Fs cache. {#fscache-clear}

This clears the fs cache. This is where remotes created from backends
are cached for a short while to make repeated rc calls more efficient.

If you change the parameters of a backend then you may want to call
this to clear an existing remote out of the cache before re-creating
it.

**Authentication is required for this call.**

### fscache/entries: Returns the number of entries in the fs cache. {#fscache-entries}

This returns the number of entries in the fs cache.

Returns
- entries - number of items in the cache

**Authentication is required for this call.**

### job/list: Lists the IDs of the running jobs {#job-list}

Parameters: None.

Results:

- jobids - array of integer job ids.

### job/status: Reads the status of the job ID {#job-status}

Parameters:

- jobid - id of the job (integer).

Results:

- finished - boolean
- duration - time in seconds that the job ran for
- endTime - time the job finished (e.g. "2018-10-26T18:50:20.528746884+01:00")
- error - error from the job or empty string for no error
- finished - boolean whether the job has finished or not
- id - as passed in above
- startTime - time the job started (e.g. "2018-10-26T18:50:20.528336039+01:00")
- success - boolean - true for success false otherwise
- output - output of the job as would have been returned if called synchronously
- progress - output of the progress related to the underlying job

### job/stop: Stop the running job {#job-stop}

Parameters:

- jobid - id of the job (integer).

### mount/listmounts: Show current mount points {#mount-listmounts}

This shows currently mounted points, which can be used for performing an unmount.

This takes no parameters and returns

- mountPoints: list of current mount points

Eg

    rclone rc mount/listmounts

**Authentication is required for this call.**

### mount/mount: Create a new mount point {#mount-mount}

rclone allows Linux, FreeBSD, macOS and Windows to mount any of
Rclone's cloud storage systems as a file system with FUSE.

If no mountType is provided, the priority is given as follows: 1. mount 2.cmount 3.mount2

This takes the following parameters:

- fs - a remote path to be mounted (required)
- mountPoint: valid path on the local machine (required)
- mountType: one of the values (mount, cmount, mount2) specifies the mount implementation to use
- mountOpt: a JSON object with Mount options in.
- vfsOpt: a JSON object with VFS options in.

Example:

    rclone rc mount/mount fs=mydrive: mountPoint=/home/<user>/mountPoint
    rclone rc mount/mount fs=mydrive: mountPoint=/home/<user>/mountPoint mountType=mount
    rclone rc mount/mount fs=TestDrive: mountPoint=/mnt/tmp vfsOpt='{"CacheMode": 2}' mountOpt='{"AllowOther": true}'

The vfsOpt are as described in options/get and can be seen in the the
"vfs" section when running and the mountOpt can be seen in the "mount" section:

    rclone rc options/get

**Authentication is required for this call.**

### mount/types: Show all possible mount types {#mount-types}

This shows all possible mount types and returns them as a list.

This takes no parameters and returns

- mountTypes: list of mount types

The mount types are strings like "mount", "mount2", "cmount" and can
be passed to mount/mount as the mountType parameter.

Eg

    rclone rc mount/types

**Authentication is required for this call.**

### mount/unmount: Unmount selected active mount {#mount-unmount}

rclone allows Linux, FreeBSD, macOS and Windows to
mount any of Rclone's cloud storage systems as a file system with
FUSE.

This takes the following parameters:

- mountPoint: valid path on the local machine where the mount was created (required)

Example:

    rclone rc mount/unmount mountPoint=/home/<user>/mountPoint

**Authentication is required for this call.**

### mount/unmountall: Show current mount points {#mount-unmountall}

This shows currently mounted points, which can be used for performing an unmount.

This takes no parameters and returns error if unmount does not succeed.

Eg

    rclone rc mount/unmountall

**Authentication is required for this call.**

### operations/about: Return the space used on the remote {#operations-about}

This takes the following parameters:

- fs - a remote name string e.g. "drive:"

The result is as returned from rclone about --json

See the [about command](/commands/rclone_size/) command for more information on the above.

**Authentication is required for this call.**

### operations/cleanup: Remove trashed files in the remote or path {#operations-cleanup}

This takes the following parameters:

- fs - a remote name string e.g. "drive:"

See the [cleanup command](/commands/rclone_cleanup/) command for more information on the above.

**Authentication is required for this call.**

### operations/copyfile: Copy a file from source remote to destination remote {#operations-copyfile}

This takes the following parameters:

- srcFs - a remote name string e.g. "drive:" for the source
- srcRemote - a path within that remote e.g. "file.txt" for the source
- dstFs - a remote name string e.g. "drive2:" for the destination
- dstRemote - a path within that remote e.g. "file2.txt" for the destination

**Authentication is required for this call.**

### operations/copyurl: Copy the URL to the object {#operations-copyurl}

This takes the following parameters:

- fs - a remote name string e.g. "drive:"
- remote - a path within that remote e.g. "dir"
- url - string, URL to read from
 - autoFilename - boolean, set to true to retrieve destination file name from url
See the [copyurl command](/commands/rclone_copyurl/) command for more information on the above.

**Authentication is required for this call.**

### operations/delete: Remove files in the path {#operations-delete}

This takes the following parameters:

- fs - a remote name string e.g. "drive:"

See the [delete command](/commands/rclone_delete/) command for more information on the above.

**Authentication is required for this call.**

### operations/deletefile: Remove the single file pointed to {#operations-deletefile}

This takes the following parameters:

- fs - a remote name string e.g. "drive:"
- remote - a path within that remote e.g. "dir"

See the [deletefile command](/commands/rclone_deletefile/) command for more information on the above.

**Authentication is required for this call.**

### operations/fsinfo: Return information about the remote {#operations-fsinfo}

This takes the following parameters:

- fs - a remote name string e.g. "drive:"

This returns info about the remote passed in;

```
{
	// optional features and whether they are available or not
	"Features": {
		"About": true,
		"BucketBased": false,
		"CanHaveEmptyDirectories": true,
		"CaseInsensitive": false,
		"ChangeNotify": false,
		"CleanUp": false,
		"Copy": false,
		"DirCacheFlush": false,
		"DirMove": true,
		"DuplicateFiles": false,
		"GetTier": false,
		"ListR": false,
		"MergeDirs": false,
		"Move": true,
		"OpenWriterAt": true,
		"PublicLink": false,
		"Purge": true,
		"PutStream": true,
		"PutUnchecked": false,
		"ReadMimeType": false,
		"ServerSideAcrossConfigs": false,
		"SetTier": false,
		"SetWrapper": false,
		"UnWrap": false,
		"WrapFs": false,
		"WriteMimeType": false
	},
	// Names of hashes available
	"Hashes": [
		"MD5",
		"SHA-1",
		"DropboxHash",
		"QuickXorHash"
	],
	"Name": "local",	// Name as created
	"Precision": 1,		// Precision of timestamps in ns
	"Root": "/",		// Path as created
	"String": "Local file system at /" // how the remote will appear in logs
}
```

This command does not have a command line equivalent so use this instead:

    rclone rc --loopback operations/fsinfo fs=remote:

### operations/list: List the given remote and path in JSON format {#operations-list}

This takes the following parameters:

- fs - a remote name string e.g. "drive:"
- remote - a path within that remote e.g. "dir"
- opt - a dictionary of options to control the listing (optional)
    - recurse - If set recurse directories
    - noModTime - If set return modification time
    - showEncrypted -  If set show decrypted names
    - showOrigIDs - If set show the IDs for each item if known
    - showHash - If set return a dictionary of hashes
    - noMimeType - If set don't show mime types
    - dirsOnly - If set only show directories
    - filesOnly - If set only show files
    - hashTypes - array of strings of hash types to show if showHash set

Returns:

- list
    - This is an array of objects as described in the lsjson command

See the [lsjson command](/commands/rclone_lsjson/) for more information on the above and examples.

**Authentication is required for this call.**

### operations/mkdir: Make a destination directory or container {#operations-mkdir}

This takes the following parameters:

- fs - a remote name string e.g. "drive:"
- remote - a path within that remote e.g. "dir"

See the [mkdir command](/commands/rclone_mkdir/) command for more information on the above.

**Authentication is required for this call.**

### operations/movefile: Move a file from source remote to destination remote {#operations-movefile}

This takes the following parameters:

- srcFs - a remote name string e.g. "drive:" for the source
- srcRemote - a path within that remote e.g. "file.txt" for the source
- dstFs - a remote name string e.g. "drive2:" for the destination
- dstRemote - a path within that remote e.g. "file2.txt" for the destination

**Authentication is required for this call.**

### operations/publiclink: Create or retrieve a public link to the given file or folder. {#operations-publiclink}

This takes the following parameters:

- fs - a remote name string e.g. "drive:"
- remote - a path within that remote e.g. "dir"
- unlink - boolean - if set removes the link rather than adding it (optional)
- expire - string - the expiry time of the link e.g. "1d" (optional)

Returns:

- url - URL of the resource

See the [link command](/commands/rclone_link/) command for more information on the above.

**Authentication is required for this call.**

### operations/purge: Remove a directory or container and all of its contents {#operations-purge}

This takes the following parameters:

- fs - a remote name string e.g. "drive:"
- remote - a path within that remote e.g. "dir"

See the [purge command](/commands/rclone_purge/) command for more information on the above.

**Authentication is required for this call.**

### operations/rmdir: Remove an empty directory or container {#operations-rmdir}

This takes the following parameters:

- fs - a remote name string e.g. "drive:"
- remote - a path within that remote e.g. "dir"

See the [rmdir command](/commands/rclone_rmdir/) command for more information on the above.

**Authentication is required for this call.**

### operations/rmdirs: Remove all the empty directories in the path {#operations-rmdirs}

This takes the following parameters:

- fs - a remote name string e.g. "drive:"
- remote - a path within that remote e.g. "dir"
- leaveRoot - boolean, set to true not to delete the root
See the [rmdirs command](/commands/rclone_rmdirs/) command for more information on the above.

**Authentication is required for this call.**

### operations/size: Count the number of bytes and files in remote {#operations-size}

This takes the following parameters:

- fs - a remote name string e.g. "drive:path/to/dir"

Returns:

- count - number of files
- bytes - number of bytes in those files

See the [size command](/commands/rclone_size/) command for more information on the above.

**Authentication is required for this call.**

### operations/stat: Give information about the supplied file or directory {#operations-stat}

This takes the following parameters

- fs - a remote name string eg "drive:"
- remote - a path within that remote eg "dir"
- opt - a dictionary of options to control the listing (optional)
    - see operations/list for the options

The result is

- item - an object as described in the lsjson command. Will be null if not found.

Note that if you are only interested in files then it is much more
efficient to set the filesOnly flag in the options.

See the [lsjson command](/commands/rclone_lsjson/) for more information on the above and examples.

**Authentication is required for this call.**

### operations/uploadfile: Upload file using multiform/form-data {#operations-uploadfile}

This takes the following parameters:

- fs - a remote name string e.g. "drive:"
- remote - a path within that remote e.g. "dir"
- each part in body represents a file to be uploaded
See the [uploadfile command](/commands/rclone_uploadfile/) command for more information on the above.

**Authentication is required for this call.**

### options/blocks: List all the option blocks {#options-blocks}

Returns:
- options - a list of the options block names

### options/get: Get all the global options {#options-get}

Returns an object where keys are option block names and values are an
object with the current option values in.

Note that these are the global options which are unaffected by use of
the _config and _filter parameters. If you wish to read the parameters
set in _config then use options/config and for _filter use options/filter.

This shows the internal names of the option within rclone which should
map to the external options very easily with a few exceptions.

### options/local: Get the currently active config for this call {#options-local}

Returns an object with the keys "config" and "filter".
The "config" key contains the local config and the "filter" key contains
the local filters.

Note that these are the local options specific to this rc call. If
_config was not supplied then they will be the global options.
Likewise with "_filter".

This call is mostly useful for seeing if _config and _filter passing
is working.

This shows the internal names of the option within rclone which should
map to the external options very easily with a few exceptions.

### options/set: Set an option {#options-set}

Parameters:

- option block name containing an object with
  - key: value

Repeated as often as required.

Only supply the options you wish to change.  If an option is unknown
it will be silently ignored.  Not all options will have an effect when
changed like this.

For example:

This sets DEBUG level logs (-vv) (these can be set by number or string)

    rclone rc options/set --json '{"main": {"LogLevel": "DEBUG"}}'
    rclone rc options/set --json '{"main": {"LogLevel": 8}}'

And this sets INFO level logs (-v)

    rclone rc options/set --json '{"main": {"LogLevel": "INFO"}}'

And this sets NOTICE level logs (normal without -v)

    rclone rc options/set --json '{"main": {"LogLevel": "NOTICE"}}'

### pluginsctl/addPlugin: Add a plugin using url {#pluginsctl-addPlugin}

Used for adding a plugin to the webgui.

This takes the following parameters:

- url - http url of the github repo where the plugin is hosted (http://github.com/rclone/rclone-webui-react).

Example:

   rclone rc pluginsctl/addPlugin

**Authentication is required for this call.**

### pluginsctl/getPluginsForType: Get plugins with type criteria {#pluginsctl-getPluginsForType}

This shows all possible plugins by a mime type.

This takes the following parameters:

- type - supported mime type by a loaded plugin e.g. (video/mp4, audio/mp3).
- pluginType - filter plugins based on their type e.g. (DASHBOARD, FILE_HANDLER, TERMINAL).

Returns:

- loadedPlugins - list of current production plugins.
- testPlugins - list of temporarily loaded development plugins, usually running on a different server.

Example:

   rclone rc pluginsctl/getPluginsForType type=video/mp4

**Authentication is required for this call.**

### pluginsctl/listPlugins: Get the list of currently loaded plugins {#pluginsctl-listPlugins}

This allows you to get the currently enabled plugins and their details.

This takes no parameters and returns:

- loadedPlugins - list of current production plugins.
- testPlugins - list of temporarily loaded development plugins, usually running on a different server.

E.g.

   rclone rc pluginsctl/listPlugins

**Authentication is required for this call.**

### pluginsctl/listTestPlugins: Show currently loaded test plugins {#pluginsctl-listTestPlugins}

Allows listing of test plugins with the rclone.test set to true in package.json of the plugin.

This takes no parameters and returns:

- loadedTestPlugins - list of currently available test plugins.

E.g.

    rclone rc pluginsctl/listTestPlugins

**Authentication is required for this call.**

### pluginsctl/removePlugin: Remove a loaded plugin {#pluginsctl-removePlugin}

This allows you to remove a plugin using it's name.

This takes parameters:

- name - name of the plugin in the format `author`/`plugin_name`.

E.g.

   rclone rc pluginsctl/removePlugin name=rclone/video-plugin

**Authentication is required for this call.**

### pluginsctl/removeTestPlugin: Remove  a test plugin {#pluginsctl-removeTestPlugin}

This allows you to remove a plugin using it's name.

This takes the following parameters:

- name - name of the plugin in the format `author`/`plugin_name`.

Example:

    rclone rc pluginsctl/removeTestPlugin name=rclone/rclone-webui-react

**Authentication is required for this call.**

### rc/error: This returns an error {#rc-error}

This returns an error with the input as part of its error string.
Useful for testing error handling.

### rc/list: List all the registered remote control commands {#rc-list}

This lists all the registered remote control commands as a JSON map in
the commands response.

### rc/noop: Echo the input to the output parameters {#rc-noop}

This echoes the input parameters to the output parameters for testing
purposes.  It can be used to check that rclone is still alive and to
check that parameter passing is working properly.

### rc/noopauth: Echo the input to the output parameters requiring auth {#rc-noopauth}

This echoes the input parameters to the output parameters for testing
purposes.  It can be used to check that rclone is still alive and to
check that parameter passing is working properly.

**Authentication is required for this call.**

### sync/copy: copy a directory from source remote to destination remote {#sync-copy}

This takes the following parameters:

- srcFs - a remote name string e.g. "drive:src" for the source
- dstFs - a remote name string e.g. "drive:dst" for the destination
- createEmptySrcDirs - create empty src directories on destination if set


See the [copy command](/commands/rclone_copy/) command for more information on the above.

**Authentication is required for this call.**

### sync/move: move a directory from source remote to destination remote {#sync-move}

This takes the following parameters:

- srcFs - a remote name string e.g. "drive:src" for the source
- dstFs - a remote name string e.g. "drive:dst" for the destination
- createEmptySrcDirs - create empty src directories on destination if set
- deleteEmptySrcDirs - delete empty src directories if set


See the [move command](/commands/rclone_move/) command for more information on the above.

**Authentication is required for this call.**

### sync/sync: sync a directory from source remote to destination remote {#sync-sync}

This takes the following parameters:

- srcFs - a remote name string e.g. "drive:src" for the source
- dstFs - a remote name string e.g. "drive:dst" for the destination
- createEmptySrcDirs - create empty src directories on destination if set


See the [sync command](/commands/rclone_sync/) command for more information on the above.

**Authentication is required for this call.**

### vfs/forget: Forget files or directories in the directory cache. {#vfs-forget}

This forgets the paths in the directory cache causing them to be
re-read from the remote when needed.

If no paths are passed in then it will forget all the paths in the
directory cache.

    rclone rc vfs/forget

Otherwise pass files or dirs in as file=path or dir=path.  Any
parameter key starting with file will forget that file and any
starting with dir will forget that dir, e.g.

    rclone rc vfs/forget file=hello file2=goodbye dir=home/junk
 
This command takes an "fs" parameter. If this parameter is not
supplied and if there is only one VFS in use then that VFS will be
used. If there is more than one VFS in use then the "fs" parameter
must be supplied.

### vfs/list: List active VFSes. {#vfs-list}

This lists the active VFSes.

It returns a list under the key "vfses" where the values are the VFS
names that could be passed to the other VFS commands in the "fs"
parameter.

### vfs/poll-interval: Get the status or update the value of the poll-interval option. {#vfs-poll-interval}

Without any parameter given this returns the current status of the
poll-interval setting.

When the interval=duration parameter is set, the poll-interval value
is updated and the polling function is notified.
Setting interval=0 disables poll-interval.

    rclone rc vfs/poll-interval interval=5m

The timeout=duration parameter can be used to specify a time to wait
for the current poll function to apply the new value.
If timeout is less or equal 0, which is the default, wait indefinitely.

The new poll-interval value will only be active when the timeout is
not reached.

If poll-interval is updated or disabled temporarily, some changes
might not get picked up by the polling function, depending on the
used remote.
 
This command takes an "fs" parameter. If this parameter is not
supplied and if there is only one VFS in use then that VFS will be
used. If there is more than one VFS in use then the "fs" parameter
must be supplied.

### vfs/refresh: Refresh the directory cache. {#vfs-refresh}

This reads the directories for the specified paths and freshens the
directory cache.

If no paths are passed in then it will refresh the root directory.

    rclone rc vfs/refresh

Otherwise pass directories in as dir=path. Any parameter key
starting with dir will refresh that directory, e.g.

    rclone rc vfs/refresh dir=home/junk dir2=data/misc

If the parameter recursive=true is given the whole directory tree
will get refreshed. This refresh will use --fast-list if enabled.
 
This command takes an "fs" parameter. If this parameter is not
supplied and if there is only one VFS in use then that VFS will be
used. If there is more than one VFS in use then the "fs" parameter
must be supplied.

{{< rem autogenerated stop >}}

## Accessing the remote control via HTTP {#api-http}

Rclone implements a simple HTTP based protocol.

Each endpoint takes an JSON object and returns a JSON object or an
error.  The JSON objects are essentially a map of string names to
values.

All calls must made using POST.

The input objects can be supplied using URL parameters, POST
parameters or by supplying "Content-Type: application/json" and a JSON
blob in the body.  There are examples of these below using `curl`.

The response will be a JSON blob in the body of the response.  This is
formatted to be reasonably human-readable.

### Error returns

If an error occurs then there will be an HTTP error status (e.g. 500)
and the body of the response will contain a JSON encoded error object,
e.g.

```
{
    "error": "Expecting string value for key \"remote\" (was float64)",
    "input": {
        "fs": "/tmp",
        "remote": 3
    },
    "status": 400
    "path": "operations/rmdir",
}
```

The keys in the error response are
- error - error string
- input - the input parameters to the call
- status - the HTTP status code
- path - the path of the call

### CORS

The sever implements basic CORS support and allows all origins for that.
The response to a preflight OPTIONS request will echo the requested "Access-Control-Request-Headers" back.

### Using POST with URL parameters only

```
curl -X POST 'http://localhost:5572/rc/noop?potato=1&sausage=2'
```

Response

```
{
	"potato": "1",
	"sausage": "2"
}
```

Here is what an error response looks like:

```
curl -X POST 'http://localhost:5572/rc/error?potato=1&sausage=2'
```

```
{
	"error": "arbitrary error on input map[potato:1 sausage:2]",
	"input": {
		"potato": "1",
		"sausage": "2"
	}
}
```

Note that curl doesn't return errors to the shell unless you use the `-f` option

```
$ curl -f -X POST 'http://localhost:5572/rc/error?potato=1&sausage=2'
curl: (22) The requested URL returned error: 400 Bad Request
$ echo $?
22
```

### Using POST with a form

```
curl --data "potato=1" --data "sausage=2" http://localhost:5572/rc/noop
```

Response

```
{
	"potato": "1",
	"sausage": "2"
}
```

Note that you can combine these with URL parameters too with the POST
parameters taking precedence.

```
curl --data "potato=1" --data "sausage=2" "http://localhost:5572/rc/noop?rutabaga=3&sausage=4"
```

Response

```
{
	"potato": "1",
	"rutabaga": "3",
	"sausage": "4"
}

```

### Using POST with a JSON blob

```
curl -H "Content-Type: application/json" -X POST -d '{"potato":2,"sausage":1}' http://localhost:5572/rc/noop
```

response

```
{
	"password": "xyz",
	"username": "xyz"
}
```

This can be combined with URL parameters too if required.  The JSON
blob takes precedence.

```
curl -H "Content-Type: application/json" -X POST -d '{"potato":2,"sausage":1}' 'http://localhost:5572/rc/noop?rutabaga=3&potato=4'
```

```
{
	"potato": 2,
	"rutabaga": "3",
	"sausage": 1
}
```

## Debugging rclone with pprof ##

If you use the `--rc` flag this will also enable the use of the go
profiling tools on the same port.

To use these, first [install go](https://golang.org/doc/install).

### Debugging memory use

To profile rclone's memory use you can run:

    go tool pprof -web http://localhost:5572/debug/pprof/heap

This should open a page in your browser showing what is using what
memory.

You can also use the `-text` flag to produce a textual summary

```
$ go tool pprof -text http://localhost:5572/debug/pprof/heap
Showing nodes accounting for 1537.03kB, 100% of 1537.03kB total
      flat  flat%   sum%        cum   cum%
 1024.03kB 66.62% 66.62%  1024.03kB 66.62%  github.com/rclone/rclone/vendor/golang.org/x/net/http2/hpack.addDecoderNode
     513kB 33.38%   100%      513kB 33.38%  net/http.newBufioWriterSize
         0     0%   100%  1024.03kB 66.62%  github.com/rclone/rclone/cmd/all.init
         0     0%   100%  1024.03kB 66.62%  github.com/rclone/rclone/cmd/serve.init
         0     0%   100%  1024.03kB 66.62%  github.com/rclone/rclone/cmd/serve/restic.init
         0     0%   100%  1024.03kB 66.62%  github.com/rclone/rclone/vendor/golang.org/x/net/http2.init
         0     0%   100%  1024.03kB 66.62%  github.com/rclone/rclone/vendor/golang.org/x/net/http2/hpack.init
         0     0%   100%  1024.03kB 66.62%  github.com/rclone/rclone/vendor/golang.org/x/net/http2/hpack.init.0
         0     0%   100%  1024.03kB 66.62%  main.init
         0     0%   100%      513kB 33.38%  net/http.(*conn).readRequest
         0     0%   100%      513kB 33.38%  net/http.(*conn).serve
         0     0%   100%  1024.03kB 66.62%  runtime.main
```

### Debugging go routine leaks

Memory leaks are most often caused by go routine leaks keeping memory
alive which should have been garbage collected.

See all active go routines using

    curl http://localhost:5572/debug/pprof/goroutine?debug=1

Or go to http://localhost:5572/debug/pprof/goroutine?debug=1 in your browser.

### Other profiles to look at

You can see a summary of profiles available at http://localhost:5572/debug/pprof/

Here is how to use some of them:

- Memory: `go tool pprof http://localhost:5572/debug/pprof/heap`
- Go routines: `curl http://localhost:5572/debug/pprof/goroutine?debug=1`
- 30-second CPU profile: `go tool pprof http://localhost:5572/debug/pprof/profile`
- 5-second execution trace: `wget http://localhost:5572/debug/pprof/trace?seconds=5`
- Goroutine blocking profile
    - Enable first with: `rclone rc debug/set-block-profile-rate rate=1` ([docs](#debug/set-block-profile-rate))
    - `go tool pprof http://localhost:5572/debug/pprof/block`
- Contended mutexes:
    - Enable first with: `rclone rc debug/set-mutex-profile-fraction rate=1` ([docs](#debug/set-mutex-profile-fraction))
    - `go tool pprof http://localhost:5572/debug/pprof/mutex`

See the [net/http/pprof docs](https://golang.org/pkg/net/http/pprof/)
for more info on how to use the profiling and for a general overview
see [the Go team's blog post on profiling go programs](https://blog.golang.org/profiling-go-programs).

The profiling hook is [zero overhead unless it is used](https://stackoverflow.com/q/26545159/164234).

