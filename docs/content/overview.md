---
title: "Overview of cloud storage systems"
description: "Overview of cloud storage systems"
type: page
---

# Overview of cloud storage systems #

Each cloud storage system is slightly different.  Rclone attempts to
provide a unified interface to them, but some underlying differences
show through.

## Features ##

Here is an overview of the major features of each cloud storage system.

| Name                         | Hash        | ModTime | Case Insensitive | Duplicate Files | MIME Type |
| ---------------------------- |:-----------:|:-------:|:----------------:|:---------------:|:---------:|
| 1Fichier                     | Whirlpool   | No      | No               | Yes             | R         |
| Amazon Drive                 | MD5         | No      | Yes              | No              | R         |
| Amazon S3 (or S3 compatible) | MD5         | Yes     | No               | No              | R/W       |
| Backblaze B2                 | SHA1        | Yes     | No               | No              | R/W       |
| Box                          | SHA1        | Yes     | Yes              | No              | -         |
| Citrix ShareFile             | MD5         | Yes     | Yes              | No              | -         |
| Dropbox                      | DBHASH ¹    | Yes     | Yes              | No              | -         |
| Enterprise File Fabric       | -           | Yes     | Yes              | No              | R/W       |
| FTP                          | -           | No      | No               | No              | -         |
| Google Cloud Storage         | MD5         | Yes     | No               | No              | R/W       |
| Google Drive                 | MD5         | Yes     | No               | Yes             | R/W       |
| Google Photos                | -           | No      | No               | Yes             | R         |
| HDFS                         | -           | Yes     | No               | No              | -         |
| HTTP                         | -           | No      | No               | No              | R         |
| Hubic                        | MD5         | Yes     | No               | No              | R/W       |
| Jottacloud                   | MD5         | Yes     | Yes              | No              | R         |
| Koofr                        | MD5         | No      | Yes              | No              | -         |
| Mail.ru Cloud                | Mailru ⁶    | Yes     | Yes              | No              | -         |
| Mega                         | -           | No      | No               | Yes             | -         |
| Memory                       | MD5         | Yes     | No               | No              | -         |
| Microsoft Azure Blob Storage | MD5         | Yes     | No               | No              | R/W       |
| Microsoft OneDrive           | SHA1 ⁵      | Yes     | Yes              | No              | R         |
| OpenDrive                    | MD5         | Yes     | Yes              | Partial ⁸       | -         |
| OpenStack Swift              | MD5         | Yes     | No               | No              | R/W       |
| pCloud                       | MD5, SHA1 ⁷ | Yes     | No               | No              | W         |
| premiumize.me                | -           | No      | Yes              | No              | R         |
| put.io                       | CRC-32      | Yes     | No               | Yes             | R         |
| QingStor                     | MD5         | No      | No               | No              | R/W       |
| Seafile                      | -           | No      | No               | No              | -         |
| SFTP                         | MD5, SHA1 ² | Yes     | Depends          | No              | -         |
| Sia                          | -           | No      | No               | No              | -         |
| SugarSync                    | -           | No      | No               | No              | -         |
| Tardigrade                   | -           | Yes     | No               | No              | -         |
| Uptobox                      | -           | No      | No               | Yes             | -         |
| WebDAV                       | MD5, SHA1 ³ | Yes ⁴   | Depends          | No              | -         |
| Yandex Disk                  | MD5         | Yes     | No               | No              | R         |
| Zoho WorkDrive               | -           | No      | No               | No              | -         |
| The local filesystem         | All         | Yes     | Depends          | No              | -         |

### Notes

¹ Dropbox supports [its own custom
hash](https://www.dropbox.com/developers/reference/content-hash).
This is an SHA256 sum of all the 4 MiB block SHA256s.

² SFTP supports checksums if the same login has shell access and
`md5sum` or `sha1sum` as well as `echo` are in the remote's PATH.

³ WebDAV supports hashes when used with Owncloud and Nextcloud only.

⁴ WebDAV supports modtimes when used with Owncloud and Nextcloud only.

⁵ Microsoft OneDrive Personal supports SHA1 hashes, whereas OneDrive
for business and SharePoint server support Microsoft's own
[QuickXorHash](https://docs.microsoft.com/en-us/onedrive/developer/code-snippets/quickxorhash).

⁶ Mail.ru uses its own modified SHA1 hash

⁷ pCloud only supports SHA1 (not MD5) in its EU region

⁸ Opendrive does not support creation of duplicate files using
their web client interface or other stock clients, but the underlying
storage platform has been determined to allow duplicate files, and it
is possible to create them with `rclone`.  It may be that this is a
mistake or an unsupported feature.

### Hash ###

The cloud storage system supports various hash types of the objects.
The hashes are used when transferring data as an integrity check and
can be specifically used with the `--checksum` flag in syncs and in
the `check` command.

To use the verify checksums when transferring between cloud storage
systems they must support a common hash type.

### ModTime ###

The cloud storage system supports setting modification times on
objects.  If it does then this enables a using the modification times
as part of the sync.  If not then only the size will be checked by
default, though the MD5SUM can be checked with the `--checksum` flag.

All cloud storage systems support some kind of date on the object and
these will be set when transferring from the cloud storage system.

### Case Insensitive ###

If a cloud storage systems is case sensitive then it is possible to
have two files which differ only in case, e.g. `file.txt` and
`FILE.txt`.  If a cloud storage system is case insensitive then that
isn't possible.

This can cause problems when syncing between a case insensitive
system and a case sensitive system.  The symptom of this is that no
matter how many times you run the sync it never completes fully.

The local filesystem and SFTP may or may not be case sensitive
depending on OS.

  * Windows - usually case insensitive, though case is preserved
  * OSX - usually case insensitive, though it is possible to format case sensitive
  * Linux - usually case sensitive, but there are case insensitive file systems (e.g. FAT formatted USB keys)

Most of the time this doesn't cause any problems as people tend to
avoid files whose name differs only by case even on case sensitive
systems.

### Duplicate files ###

If a cloud storage system allows duplicate files then it can have two
objects with the same name.

This confuses rclone greatly when syncing - use the `rclone dedupe`
command to rename or remove duplicates.

### Restricted filenames ###

Some cloud storage systems might have restrictions on the characters
that are usable in file or directory names.
When `rclone` detects such a name during a file upload, it will
transparently replace the restricted characters with similar looking
Unicode characters. To handle the different sets of restricted characters
for different backends, rclone uses something it calls [encoding](#encoding).

This process is designed to avoid ambiguous file names as much as
possible and allow to move files between many cloud storage systems
transparently.

The name shown by `rclone` to the user or during log output will only
contain a minimal set of [replaced characters](#restricted-characters)
to ensure correct formatting and not necessarily the actual name used
on the cloud storage.

This transformation is reversed when downloading a file or parsing
`rclone` arguments. For example, when uploading a file named `my file?.txt`
to Onedrive, it will be displayed as `my file?.txt` on the console, but
stored as `my file？.txt` to Onedrive (the `?` gets replaced by the similar
looking `？` character, the so-called "fullwidth question mark").
The reverse transformation allows to read a file `unusual/name.txt`
from Google Drive, by passing the name `unusual／name.txt` on the command line
(the `/` needs to be replaced by the similar looking `／` character).

#### Caveats {#restricted-filenames-caveats}

The filename encoding system works well in most cases, at least
where file names are written in English or similar languages.
You might not even notice it: It just works. In some cases it may
lead to issues, though. E.g. when file names are written in Chinese,
or Japanese, where it is always the Unicode fullwidth variants of the
punctuation marks that are used.

On Windows, the characters `:`, `*` and `?` are examples of restricted
characters. If these are used in filenames on a remote that supports it,
Rclone will transparently convert them to their fullwidth Unicode
variants `＊`, `？` and `：` when downloading to Windows, and back again
when uploading. This way files with names that are not allowed on Windows
can still be stored.

However, if you have files on your Windows system originally with these same
Unicode characters in their names, they will be included in the same conversion
process. E.g. if you create a file in your Windows filesystem with name
`Test：1.jpg`, where `：` is the Unicode fullwidth colon symbol, and use
rclone to upload it to Google Drive, which supports regular `:` (halfwidth
question mark), rclone will replace the fullwidth `:` with the
halfwidth `:` and store the file as `Test:1.jpg` in Google Drive. Since
both Windows and Google Drive allows the name `Test：1.jpg`, it would
probably be better if rclone just kept the name as is in this case.

With the opposite situation; if you have a file named `Test:1.jpg`,
in your Google Drive, e.g. uploaded from a Linux system where `:` is valid
in file names. Then later use rclone to copy this file to your Windows
computer you will notice that on your local disk it gets renamed
to `Test：1.jpg`. The original filename is not legal on Windows, due to
the `:`, and rclone therefore renames it to make the copy possible.
That is all good. However, this can also lead to an issue: If you already
had a *different* file named `Test：1.jpg` on Windows, and then use rclone
to copy either way. Rclone will then treat the file originally named
`Test:1.jpg` on Google Drive and the file originally named `Test：1.jpg`
on Windows as the same file, and replace the contents from one with the other.

Its virtually impossible to handle all cases like these correctly in all
situations, but by customizing the [encoding option](#encoding), changing the
set of characters that rclone should convert, you should be able to
create a configuration that works well for your specific situation.
See also the [example](/overview/#encoding-example-windows) below.

(Windows was used as an example of a file system with many restricted
characters, and Google drive a storage system with few.)

#### Default restricted characters {#restricted-characters}

The table below shows the characters that are replaced by default.

When a replacement character is found in a filename, this character
will be escaped with the `‛` character to avoid ambiguous file names.
(e.g. a file named `␀.txt` would shown as `‛␀.txt`)

Each cloud storage backend can use a different set of characters,
which will be specified in the documentation for each backend.

| Character | Value | Replacement |
| --------- |:-----:|:-----------:|
| NUL       | 0x00  | ␀           |
| SOH       | 0x01  | ␁           |
| STX       | 0x02  | ␂           |
| ETX       | 0x03  | ␃           |
| EOT       | 0x04  | ␄           |
| ENQ       | 0x05  | ␅           |
| ACK       | 0x06  | ␆           |
| BEL       | 0x07  | ␇           |
| BS        | 0x08  | ␈           |
| HT        | 0x09  | ␉           |
| LF        | 0x0A  | ␊           |
| VT        | 0x0B  | ␋           |
| FF        | 0x0C  | ␌           |
| CR        | 0x0D  | ␍           |
| SO        | 0x0E  | ␎           |
| SI        | 0x0F  | ␏           |
| DLE       | 0x10  | ␐           |
| DC1       | 0x11  | ␑           |
| DC2       | 0x12  | ␒           |
| DC3       | 0x13  | ␓           |
| DC4       | 0x14  | ␔           |
| NAK       | 0x15  | ␕           |
| SYN       | 0x16  | ␖           |
| ETB       | 0x17  | ␗           |
| CAN       | 0x18  | ␘           |
| EM        | 0x19  | ␙           |
| SUB       | 0x1A  | ␚           |
| ESC       | 0x1B  | ␛           |
| FS        | 0x1C  | ␜           |
| GS        | 0x1D  | ␝           |
| RS        | 0x1E  | ␞           |
| US        | 0x1F  | ␟           |
| /         | 0x2F  | ／           |
| DEL       | 0x7F  | ␡           |

The default encoding will also encode these file names as they are
problematic with many cloud storage systems.

| File name | Replacement |
| --------- |:-----------:|
| .         | ．          |
| ..        | ．．         |

#### Invalid UTF-8 bytes {#invalid-utf8}

Some backends only support a sequence of well formed UTF-8 bytes
as file or directory names.

In this case all invalid UTF-8 bytes will be replaced with a quoted
representation of the byte value to allow uploading a file to such a
backend. For example, the invalid byte `0xFE` will be encoded as `‛FE`.

A common source of invalid UTF-8 bytes are local filesystems, that store
names in a different encoding than UTF-8 or UTF-16, like latin1. See the
[local filenames](/local/#filenames) section for details.

#### Encoding option {#encoding}

Most backends have an encoding option, specified as a flag
`--backend-encoding` where `backend` is the name of the backend, or as
a config parameter `encoding` (you'll need to select the Advanced
config in `rclone config` to see it).

This will have default value which encodes and decodes characters in
such a way as to preserve the maximum number of characters (see
above).

However this can be incorrect in some scenarios, for example if you
have a Windows file system with Unicode fullwidth characters
`＊`, `？` or `：`, that you want to remain as those characters on the
remote rather than being translated to regular (halfwidth) `*`, `?` and `:`.

The `--backend-encoding` flags allow you to change that. You can
disable the encoding completely with `--backend-encoding None` or set
`encoding = None` in the config file.

Encoding takes a comma separated list of encodings. You can see the
list of all possible values by passing an invalid value to this
flag, e.g. `--local-encoding "help"`. The command `rclone help flags encoding`
will show you the defaults for the backends.

| Encoding  | Characters |
| --------- | ---------- |
| Asterisk | `*` |
| BackQuote | `` ` `` |
| BackSlash | `\` |
| Colon | `:` |
| CrLf | CR 0x0D, LF 0x0A |
| Ctl | All control characters 0x00-0x1F |
| Del | DEL 0x7F |
| Dollar | `$` |
| Dot | `.` or `..` as entire string |
| DoubleQuote | `"` |
| Hash | `#` |
| InvalidUtf8 | An invalid UTF-8 character (e.g. latin1) |
| LeftCrLfHtVt | CR 0x0D, LF 0x0A,HT 0x09, VT 0x0B on the left of a string |
| LeftPeriod | `.` on the left of a string |
| LeftSpace | SPACE on the left of a string |
| LeftTilde | `~` on the left of a string |
| LtGt | `<`, `>` |
| None | No characters are encoded |
| Percent | `%` |
| Pipe | \| |
| Question | `?` |
| RightCrLfHtVt | CR 0x0D, LF 0x0A, HT 0x09, VT 0x0B on the right of a string |
| RightPeriod | `.` on the right of a string |
| RightSpace | SPACE on the right of a string |
| SingleQuote | `'` |
| Slash | `/` |
| SquareBracket | `[`, `]` |

##### Encoding example: FTP

To take a specific example, the FTP backend's default encoding is

    --ftp-encoding "Slash,Del,Ctl,RightSpace,Dot"

However, let's say the FTP server is running on Windows and can't have
any of the invalid Windows characters in file names. You are backing
up Linux servers to this FTP server which do have those characters in
file names. So you would add the Windows set which are

    Slash,LtGt,DoubleQuote,Colon,Question,Asterisk,Pipe,BackSlash,Ctl,RightSpace,RightPeriod,InvalidUtf8,Dot

to the existing ones, giving:

    Slash,LtGt,DoubleQuote,Colon,Question,Asterisk,Pipe,BackSlash,Ctl,RightSpace,RightPeriod,InvalidUtf8,Dot,Del,RightSpace

This can be specified using the `--ftp-encoding` flag or using an `encoding` parameter in the config file.

##### Encoding example: Windows

As a nother example, take a Windows system where there is a file with
name `Test：1.jpg`, where `：` is the Unicode fullwidth colon symbol.
When using rclone to copy this to a remote which supports `:`,
the regular (halfwidth) colon (such as Google Drive), you will notice
that the file gets renamed to `Test:1.jpg`.

To avoid this you can change the set of characters rclone should convert
for the local filesystem, using command-line argument `--local-encoding`.
Rclone's default behavior on Windows corresponds to

```
--local-encoding "Slash,LtGt,DoubleQuote,Colon,Question,Asterisk,Pipe,BackSlash,Ctl,RightSpace,RightPeriod,InvalidUtf8,Dot"
```

If you want to use fullwidth characters `：`, `＊` and `？` in your filenames
without rclone changing them when uploading to a remote, then set the same as
the default value but without `Colon,Question,Asterisk`:

```
--local-encoding "Slash,LtGt,DoubleQuote,Pipe,BackSlash,Ctl,RightSpace,RightPeriod,InvalidUtf8,Dot"
```

Alternatively, you can disable the conversion of any characters with `--local-encoding None`.

Instead of using command-line argument `--local-encoding`, you may also set it
as [environment variable](/docs/#environment-variables) `RCLONE_LOCAL_ENCODING`,
or [configure](/docs/#configure) a remote of type `local` in your config,
and set the `encoding` option there.

The risk by doing this is that if you have a filename with the regular (halfwidth)
`:`, `*` and `?` in your cloud storage, and you try to download
it to your Windows filesystem, this will fail. These characters are not
valid in filenames on Windows, and you have told rclone not to work around
this by converting them to valid fullwidth variants.

### MIME Type ###

MIME types (also known as media types) classify types of documents
using a simple text classification, e.g. `text/html` or
`application/pdf`.

Some cloud storage systems support reading (`R`) the MIME type of
objects and some support writing (`W`) the MIME type of objects.

The MIME type can be important if you are serving files directly to
HTTP from the storage system.

If you are copying from a remote which supports reading (`R`) to a
remote which supports writing (`W`) then rclone will preserve the MIME
types.  Otherwise they will be guessed from the extension, or the
remote itself may assign the MIME type.

## Optional Features ##

All rclone remotes support a base command set. Other features depend
upon backend-specific capabilities.

| Name                         | Purge | Copy | Move | DirMove | CleanUp | ListR | StreamUpload | LinkSharing  | About | EmptyDir |
| ---------------------------- |:-----:|:----:|:----:|:-------:|:-------:|:-----:|:------------:|:------------:|:-----:|:--------:|
| 1Fichier                     | No    | Yes  | Yes  | No      | No      | No    | No           | Yes          | No    | Yes      |
| Amazon Drive                 | Yes   | No   | Yes  | Yes     | No      | No    | No           | No           | No    | Yes      |
| Amazon S3                    | No    | Yes  | No   | No      | Yes     | Yes   | Yes          | Yes          | No    | No       |
| Backblaze B2                 | No    | Yes  | No   | No      | Yes     | Yes   | Yes          | Yes          | No    | No       |
| Box                          | Yes   | Yes  | Yes  | Yes     | Yes ‡‡  | No    | Yes          | Yes          | Yes   | Yes      |
| Citrix ShareFile             | Yes   | Yes  | Yes  | Yes     | No      | No    | Yes          | No           | No    | Yes      |
| Dropbox                      | Yes   | Yes  | Yes  | Yes     | No      | No    | Yes          | Yes          | Yes   | Yes      |
| Enterprise File Fabric       | Yes   | Yes  | Yes  | Yes     | Yes     | No    | No           | No           | No    | Yes      |
| FTP                          | No    | No   | Yes  | Yes     | No      | No    | Yes          | No           | No    | Yes      |
| Google Cloud Storage         | Yes   | Yes  | No   | No      | No      | Yes   | Yes          | No           | No    | No       |
| Google Drive                 | Yes   | Yes  | Yes  | Yes     | Yes     | Yes   | Yes          | Yes          | Yes   | Yes      |
| Google Photos                | No    | No   | No   | No      | No      | No    | No           | No           | No    | No       |
| HDFS                         | Yes   | No   | Yes  | Yes     | No      | No    | Yes          | No           | Yes   | Yes      |
| HTTP                         | No    | No   | No   | No      | No      | No    | No           | No           | No    | Yes      |
| Hubic                        | Yes † | Yes  | No   | No      | No      | Yes   | Yes          | No           | Yes   | No       |
| Jottacloud                   | Yes   | Yes  | Yes  | Yes     | Yes     | Yes   | No           | Yes          | Yes   | Yes      |
| Mail.ru Cloud                | Yes   | Yes  | Yes  | Yes     | Yes     | No    | No           | Yes          | Yes   | Yes      |
| Mega                         | Yes   | No   | Yes  | Yes     | Yes     | No    | No           | Yes          | Yes   | Yes      |
| Memory                       | No    | Yes  | No   | No      | No      | Yes   | Yes          | No           | No    | No       |
| Microsoft Azure Blob Storage | Yes   | Yes  | No   | No      | No      | Yes   | Yes          | No           | No    | No       |
| Microsoft OneDrive           | Yes   | Yes  | Yes  | Yes     | Yes     | No    | No           | Yes          | Yes   | Yes      |
| OpenDrive                    | Yes   | Yes  | Yes  | Yes     | No      | No    | No           | No           | No    | Yes      |
| OpenStack Swift              | Yes † | Yes  | No   | No      | No      | Yes   | Yes          | No           | Yes   | No       |
| pCloud                       | Yes   | Yes  | Yes  | Yes     | Yes     | No    | No           | Yes          | Yes   | Yes      |
| premiumize.me                | Yes   | No   | Yes  | Yes     | No      | No    | No           | Yes          | Yes   | Yes      |
| put.io                       | Yes   | No   | Yes  | Yes     | Yes     | No    | Yes          | No           | Yes   | Yes      |
| QingStor                     | No    | Yes  | No   | No      | Yes     | Yes   | No           | No           | No    | No       |
| Seafile                      | Yes   | Yes  | Yes  | Yes     | Yes     | Yes   | Yes          | Yes          | Yes   | Yes      |
| SFTP                         | No    | No   | Yes  | Yes     | No      | No    | Yes          | No           | Yes   | Yes      |
| SugarSync                    | Yes   | Yes  | Yes  | Yes     | No      | No    | Yes          | Yes          | No    | Yes      |
| Tardigrade                   | Yes † | No   | No   | No      | No      | Yes   | Yes          | No           | No    | No       |
| Uptobox                      | No    | Yes  | Yes  | Yes     | No      | No    | No           | No           | No    | No       |
| WebDAV                       | Yes   | Yes  | Yes  | Yes     | No      | No    | Yes ‡        | No           | Yes   | Yes      |
| Yandex Disk                  | Yes   | Yes  | Yes  | Yes     | Yes     | No    | Yes          | Yes          | Yes   | Yes      |
| Zoho WorkDrive               | Yes   | Yes  | Yes  | Yes     | No      | No    | No           | No           | Yes   | Yes      |
| The local filesystem         | Yes   | No   | Yes  | Yes     | No      | No    | Yes          | No           | Yes   | Yes      |

### Purge ###

This deletes a directory quicker than just deleting all the files in
the directory.

† Note Swift, Hubic, and Tardigrade implement this in order to delete
directory markers but they don't actually have a quicker way of deleting
files other than deleting them individually.

‡ StreamUpload is not supported with Nextcloud

### Copy ###

Used when copying an object to and from the same remote.  This known
as a server-side copy so you can copy a file without downloading it
and uploading it again.  It is used if you use `rclone copy` or
`rclone move` if the remote doesn't support `Move` directly.

If the server doesn't support `Copy` directly then for copy operations
the file is downloaded then re-uploaded.

### Move ###

Used when moving/renaming an object on the same remote.  This is known
as a server-side move of a file.  This is used in `rclone move` if the
server doesn't support `DirMove`.

If the server isn't capable of `Move` then rclone simulates it with
`Copy` then delete.  If the server doesn't support `Copy` then rclone
will download the file and re-upload it.

### DirMove ###

This is used to implement `rclone move` to move a directory if
possible.  If it isn't then it will use `Move` on each file (which
falls back to `Copy` then download and upload - see `Move` section).

### CleanUp ###

This is used for emptying the trash for a remote by `rclone cleanup`.

If the server can't do `CleanUp` then `rclone cleanup` will return an
error.

‡‡ Note that while Box implements this it has to delete every file
individually so it will be slower than emptying the trash via the WebUI

### ListR ###

The remote supports a recursive list to list all the contents beneath
a directory quickly.  This enables the `--fast-list` flag to work.
See the [rclone docs](/docs/#fast-list) for more details.

### StreamUpload ###

Some remotes allow files to be uploaded without knowing the file size
in advance. This allows certain operations to work without spooling the
file to local disk first, e.g. `rclone rcat`.

### LinkSharing ###

Sets the necessary permissions on a file or folder and prints a link
that allows others to access them, even if they don't have an account
on the particular cloud provider.

### About ###

Rclone `about` prints quota information for a remote. Typical output
includes bytes used, free, quota and in trash.

If a remote lacks about capability `rclone about remote:`returns
an error.

Backends without about capability cannot determine free space for an
rclone mount, or use policy `mfs` (most free space) as a member of an
rclone union remote.

See [rclone about command](https://rclone.org/commands/rclone_about/)

### EmptyDir ###

The remote supports empty directories. See [Limitations](/bugs/#limitations)
 for details. Most Object/Bucket-based remotes do not support this.
