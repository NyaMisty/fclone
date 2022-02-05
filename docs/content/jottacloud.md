---
title: "Jottacloud"
description: "Rclone docs for Jottacloud"
---

# {{< icon "fa fa-cloud" >}} Jottacloud

Jottacloud is a cloud storage service provider from a Norwegian company, using its own datacenters
in Norway. In addition to the official service at [jottacloud.com](https://www.jottacloud.com/),
it also provides white-label solutions to different companies, such as:
* Telia
  * Telia Cloud (cloud.telia.se)
  * Telia Sky (sky.telia.no)
* Tele2
  * Tele2 Cloud (mittcloud.tele2.se)
* Elkjøp (with subsidiaries):
  * Elkjøp Cloud (cloud.elkjop.no)
  * Elgiganten Sweden (cloud.elgiganten.se)
  * Elgiganten Denmark (cloud.elgiganten.dk)
  * Giganti Cloud  (cloud.gigantti.fi)
  * ELKO Clouud (cloud.elko.is)

Most of the white-label versions are supported by this backend, although may require different
authentication setup - described below.

Paths are specified as `remote:path`

Paths may be as deep as required, e.g. `remote:directory/subdirectory`.

## Authentication types

Some of the whitelabel versions uses a different authentication method than the official service,
and you have to choose the correct one when setting up the remote.

### Standard authentication

To configure Jottacloud you will need to generate a personal security token in the Jottacloud web interface.
You will the option to do in your [account security settings](https://www.jottacloud.com/web/secure)
(for whitelabel version you need to find this page in its web interface).
Note that the web interface may refer to this token as a JottaCli token.

### Legacy authentication

If you are using one of the whitelabel versions (e.g. from Elkjøp) you may not have the option
to generate a CLI token. In this case you'll have to use the legacy authentication. To do this select
yes when the setup asks for legacy authentication and enter your username and password.
The rest of the setup is identical to the default setup.

### Telia Cloud authentication

Similar to other whitelabel versions Telia Cloud doesn't offer the option of creating a CLI token, and
additionally uses a separate authentication flow where the username is generated internally. To setup
rclone to use Telia Cloud, choose Telia Cloud authentication in the setup. The rest of the setup is
identical to the default setup.

### Tele2 Cloud authentication

As Tele2-Com Hem merger was completed this authentication can be used for former Com Hem Cloud and
Tele2 Cloud customers as no support for creating a CLI token exists, and additionally uses a separate
authentication flow where the username is generated internally. To setup rclone to use Tele2 Cloud,
choose Tele2 Cloud authentication in the setup. The rest of the setup is identical to the default setup.

## Configuration

Here is an example of how to make a remote called `remote` with the default setup.  First run:

    rclone config

This will guide you through an interactive setup process:

```
No remotes found - make a new one
n) New remote
s) Set configuration password
q) Quit config
n/s/q> n
name> remote
Type of storage to configure.
Enter a string value. Press Enter for the default ("").
Choose a number from below, or type in your own value
[snip]
XX / Jottacloud
   \ "jottacloud"
[snip]
Storage> jottacloud
** See help for jottacloud backend at: https://rclone.org/jottacloud/ **

Edit advanced config? (y/n)
y) Yes
n) No
y/n> n
Remote config
Use legacy authentication?.
This is only required for certain whitelabel versions of Jottacloud and not recommended for normal users.
y) Yes
n) No (default)
y/n> n

Generate a personal login token here: https://www.jottacloud.com/web/secure
Login Token> <your token here>

Do you want to use a non standard device/mountpoint e.g. for accessing files uploaded using the official Jottacloud client?

y) Yes
n) No
y/n> y
Please select the device to use. Normally this will be Jotta
Choose a number from below, or type in an existing value
 1 > DESKTOP-3H31129
 2 > Jotta
Devices> 2
Please select the mountpoint to user. Normally this will be Archive
Choose a number from below, or type in an existing value
 1 > Archive
 2 > Links
 3 > Sync

Mountpoints> 1
--------------------
[jotta]
type = jottacloud
token = {........}
device = Jotta
mountpoint = Archive
configVersion = 1
--------------------
y) Yes this is OK
e) Edit this remote
d) Delete this remote
y/e/d> y
```
Once configured you can then use `rclone` like this,

List directories in top level of your Jottacloud

    rclone lsd remote:

List all the files in your Jottacloud

    rclone ls remote:

To copy a local directory to an Jottacloud directory called backup

    rclone copy /home/source remote:backup

### Devices and Mountpoints

The official Jottacloud client registers a device for each computer you install it on,
and then creates a mountpoint for each folder you select for Backup.
The web interface uses a special device called Jotta for the Archive and Sync mountpoints.
In most cases you'll want to use the Jotta/Archive device/mountpoint, however if you want to access
files uploaded by any of the official clients rclone provides the option to select other devices
and mountpoints during config.

The built-in Jotta device may also contain several other mountpoints, such as: Latest, Links, Shared and Trash.
These are special mountpoints with a different internal representation than the "regular" mountpoints.
Rclone will only to a very limited degree support them. Generally you should avoid these, unless you know what you
are doing.

### --fast-list

This remote supports `--fast-list` which allows you to use fewer
transactions in exchange for more memory. See the [rclone
docs](/docs/#fast-list) for more details.

Note that the implementation in Jottacloud always uses only a single
API request to get the entire list, so for large folders this could
lead to long wait time before the first results are shown.

### Modified time and hashes

Jottacloud allows modification times to be set on objects accurate to 1
second. These will be used to detect whether objects need syncing or
not.

Jottacloud supports MD5 type hashes, so you can use the `--checksum`
flag.

Note that Jottacloud requires the MD5 hash before upload so if the
source does not have an MD5 checksum then the file will be cached
temporarily on disk (wherever the `TMPDIR` environment variable points
to) before it is uploaded. Small files will be cached in memory - see
the [--jottacloud-md5-memory-limit](#jottacloud-md5-memory-limit) flag.
When uploading from local disk the source checksum is always available,
so this does not apply. Starting with rclone version 1.52 the same is
true for crypted remotes (in older versions the crypt backend would not
calculate hashes for uploads from local disk, so the Jottacloud
backend had to do it as described above).

### Restricted filename characters

In addition to the [default restricted characters set](/overview/#restricted-characters)
the following characters are also replaced:

| Character | Value | Replacement |
| --------- |:-----:|:-----------:|
| "         | 0x22  | ＂          |
| *         | 0x2A  | ＊          |
| :         | 0x3A  | ：          |
| <         | 0x3C  | ＜          |
| >         | 0x3E  | ＞          |
| ?         | 0x3F  | ？          |
| \|        | 0x7C  | ｜          |

Invalid UTF-8 bytes will also be [replaced](/overview/#invalid-utf8),
as they can't be used in XML strings.

### Deleting files

By default, rclone will send all files to the trash when deleting files. They will be permanently
deleted automatically after 30 days. You may bypass the trash and permanently delete files immediately
by using the [--jottacloud-hard-delete](#jottacloud-hard-delete) flag, or set the equivalent environment variable.
Emptying the trash is supported by the [cleanup](/commands/rclone_cleanup/) command.

### Versions

Jottacloud supports file versioning. When rclone uploads a new version of a file it creates a new version of it.
Currently rclone only supports retrieving the current version but older versions can be accessed via the Jottacloud Website.

Versioning can be disabled by `--jottacloud-no-versions` option. This is achieved by deleting the remote file prior to uploading
a new version. If the upload the fails no version of the file will be available in the remote.

### Quota information

To view your current quota you can use the `rclone about remote:`
command which will display your usage limit (unless it is unlimited)
and the current usage.

{{< rem autogenerated options start" - DO NOT EDIT - instead edit fs.RegInfo in backend/jottacloud/jottacloud.go then run make backenddocs" >}}
### Advanced options

Here are the advanced options specific to jottacloud (Jottacloud).

#### --jottacloud-md5-memory-limit

Files bigger than this will be cached on disk to calculate the MD5 if required.

- Config:      md5_memory_limit
- Env Var:     RCLONE_JOTTACLOUD_MD5_MEMORY_LIMIT
- Type:        SizeSuffix
- Default:     10Mi

#### --jottacloud-trashed-only

Only show files that are in the trash.

This will show trashed files in their original directory structure.

- Config:      trashed_only
- Env Var:     RCLONE_JOTTACLOUD_TRASHED_ONLY
- Type:        bool
- Default:     false

#### --jottacloud-hard-delete

Delete files permanently rather than putting them into the trash.

- Config:      hard_delete
- Env Var:     RCLONE_JOTTACLOUD_HARD_DELETE
- Type:        bool
- Default:     false

#### --jottacloud-upload-resume-limit

Files bigger than this can be resumed if the upload fail's.

- Config:      upload_resume_limit
- Env Var:     RCLONE_JOTTACLOUD_UPLOAD_RESUME_LIMIT
- Type:        SizeSuffix
- Default:     10Mi

#### --jottacloud-no-versions

Avoid server side versioning by deleting files and recreating files instead of overwriting them.

- Config:      no_versions
- Env Var:     RCLONE_JOTTACLOUD_NO_VERSIONS
- Type:        bool
- Default:     false

#### --jottacloud-encoding

This sets the encoding for the backend.

See the [encoding section in the overview](/overview/#encoding) for more info.

- Config:      encoding
- Env Var:     RCLONE_JOTTACLOUD_ENCODING
- Type:        MultiEncoder
- Default:     Slash,LtGt,DoubleQuote,Colon,Question,Asterisk,Pipe,Del,Ctl,InvalidUtf8,Dot

{{< rem autogenerated options stop >}}

## Limitations

Note that Jottacloud is case insensitive so you can't have a file called
"Hello.doc" and one called "hello.doc".

There are quite a few characters that can't be in Jottacloud file names. Rclone will map these names to and from an identical
looking unicode equivalent. For example if a file has a ? in it will be mapped to ？ instead.

Jottacloud only supports filenames up to 255 characters in length.

## Troubleshooting

Jottacloud exhibits some inconsistent behaviours regarding deleted files and folders which may cause Copy, Move and DirMove
operations to previously deleted paths to fail. Emptying the trash should help in such cases.
