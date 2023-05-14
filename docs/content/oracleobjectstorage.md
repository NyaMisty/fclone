---
title: "Oracle Object Storage"
description: "Rclone docs for Oracle Object Storage"
versionIntroduced: "v1.60"
---

# {{< icon "fa fa-cloud" >}} Oracle Object Storage
[Oracle Object Storage Overview](https://docs.oracle.com/en-us/iaas/Content/Object/Concepts/objectstorageoverview.htm)

[Oracle Object Storage FAQ](https://www.oracle.com/cloud/storage/object-storage/faq/)

Paths are specified as `remote:bucket` (or `remote:` for the `lsd` command.)  You may put subdirectories in 
too, e.g. `remote:bucket/path/to/dir`.

## Configuration

Here is an example of making an oracle object storage configuration. `rclone config` walks you 
through it.

Here is an example of how to make a remote called `remote`.  First run:

     rclone config

This will guide you through an interactive setup process:


```
n) New remote
d) Delete remote
r) Rename remote
c) Copy remote
s) Set configuration password
q) Quit config
e/n/d/r/c/s/q> n

Enter name for new remote.
name> remote

Option Storage.
Type of storage to configure.
Choose a number from below, or type in your own value.
[snip]
XX / Oracle Cloud Infrastructure Object Storage
   \ (oracleobjectstorage)
Storage> oracleobjectstorage

Option provider.
Choose your Auth Provider
Choose a number from below, or type in your own string value.
Press Enter for the default (env_auth).
 1 / automatically pickup the credentials from runtime(env), first one to provide auth wins
   \ (env_auth)
   / use an OCI user and an API key for authentication.
 2 | you’ll need to put in a config file your tenancy OCID, user OCID, region, the path, fingerprint to an API key.
   | https://docs.oracle.com/en-us/iaas/Content/API/Concepts/sdkconfig.htm
   \ (user_principal_auth)
   / use instance principals to authorize an instance to make API calls. 
 3 | each instance has its own identity, and authenticates using the certificates that are read from instance metadata. 
   | https://docs.oracle.com/en-us/iaas/Content/Identity/Tasks/callingservicesfrominstances.htm
   \ (instance_principal_auth)
 4 / use resource principals to make API calls
   \ (resource_principal_auth)
 5 / no credentials needed, this is typically for reading public buckets
   \ (no_auth)
provider> 2

Option namespace.
Object storage namespace
Enter a value.
namespace> idbamagbg734

Option compartment.
Object storage compartment OCID
Enter a value.
compartment> ocid1.compartment.oc1..aaaaaaaapufkxc7ame3sthry5i7ujrwfc7ejnthhu6bhanm5oqfjpyasjkba

Option region.
Object storage Region
Enter a value.
region> us-ashburn-1

Option endpoint.
Endpoint for Object storage API.
Leave blank to use the default endpoint for the region.
Enter a value. Press Enter to leave empty.
endpoint> 

Option config_file.
Full Path to OCI config file
Choose a number from below, or type in your own string value.
Press Enter for the default (~/.oci/config).
 1 / oci configuration file location
   \ (~/.oci/config)
config_file> /etc/oci/dev.conf

Option config_profile.
Profile name inside OCI config file
Choose a number from below, or type in your own string value.
Press Enter for the default (Default).
 1 / Use the default profile
   \ (Default)
config_profile> Test

Edit advanced config?
y) Yes
n) No (default)
y/n> n

Configuration complete.
Options:
- type: oracleobjectstorage
- namespace: idbamagbg734
- compartment: ocid1.compartment.oc1..aaaaaaaapufkxc7ame3sthry5i7ujrwfc7ejnthhu6bhanm5oqfjpyasjkba
- region: us-ashburn-1
- provider: user_principal_auth
- config_file: /etc/oci/dev.conf
- config_profile: Test
Keep this "remote" remote?
y) Yes this is OK (default)
e) Edit this remote
d) Delete this remote
y/e/d> y
```

See all buckets

    rclone lsd remote:

Create a new bucket

    rclone mkdir remote:bucket

List the contents of a bucket

    rclone ls remote:bucket
    rclone ls remote:bucket --max-depth 1

### OCI Authentication Provider 

OCI has various authentication methods. To learn more about authentication methods please refer [oci authentication 
methods](https://docs.oracle.com/en-us/iaas/Content/API/Concepts/sdk_authentication_methods.htm) 
These choices can be specified in the rclone config file.

Rclone supports the following OCI authentication provider.

    User Principal
    Instance Principal
    Resource Principal
    No authentication

#### Authentication provider choice: User Principal
Sample rclone config file for Authentication Provider User Principal:

    [oos]
    type = oracleobjectstorage
    namespace = id<redacted>34
    compartment = ocid1.compartment.oc1..aa<redacted>ba
    region = us-ashburn-1
    provider = user_principal_auth
    config_file = /home/opc/.oci/config
    config_profile = Default

Advantages:
- One can use this method from any server within OCI or on-premises or from other cloud provider.

Considerations:
- you need to configure user’s privileges / policy to allow access to object storage
- Overhead of managing users and keys.
- If the user is deleted, the config file will no longer work and may cause automation regressions that use the user's credentials.

####  Authentication provider choice: Instance Principal
An OCI compute instance can be authorized to use rclone by using it's identity and certificates as an instance principal. 
With this approach no credentials have to be stored and managed.

Sample rclone configuration file for Authentication Provider Instance Principal:

    [opc@rclone ~]$ cat ~/.config/rclone/rclone.conf
    [oos]
    type = oracleobjectstorage
    namespace = id<redacted>fn
    compartment = ocid1.compartment.oc1..aa<redacted>k7a
    region = us-ashburn-1
    provider = instance_principal_auth

Advantages:

- With instance principals, you don't need to configure user credentials and transfer/ save it to disk in your compute 
  instances or rotate the credentials.
- You don’t need to deal with users and keys.
- Greatly helps in automation as you don't have to manage access keys, user private keys, storing them in vault, 
  using kms etc.

Considerations:

- You need to configure a dynamic group having this instance as member and add policy to read object storage to that 
  dynamic group.
- Everyone who has access to this machine can execute the CLI commands.
- It is applicable for oci compute instances only. It cannot be used on external instance or resources.

#### Authentication provider choice: Resource Principal
Resource principal auth is very similar to instance principal auth but used for resources that are not 
compute instances such as [serverless functions](https://docs.oracle.com/en-us/iaas/Content/Functions/Concepts/functionsoverview.htm). 
To use resource principal ensure Rclone process is started with these environment variables set in its process.

    export OCI_RESOURCE_PRINCIPAL_VERSION=2.2
    export OCI_RESOURCE_PRINCIPAL_REGION=us-ashburn-1
    export OCI_RESOURCE_PRINCIPAL_PRIVATE_PEM=/usr/share/model-server/key.pem
    export OCI_RESOURCE_PRINCIPAL_RPST=/usr/share/model-server/security_token

Sample rclone configuration file for Authentication Provider Resource Principal:

    [oos]
    type = oracleobjectstorage
    namespace = id<redacted>34
    compartment = ocid1.compartment.oc1..aa<redacted>ba
    region = us-ashburn-1
    provider = resource_principal_auth

#### Authentication provider choice: No authentication
Public buckets do not require any authentication mechanism to read objects.
Sample rclone configuration file for No authentication:
    
    [oos]
    type = oracleobjectstorage
    namespace = id<redacted>34
    compartment = ocid1.compartment.oc1..aa<redacted>ba
    region = us-ashburn-1
    provider = no_auth

## Options
### Modified time

The modified time is stored as metadata on the object as
`opc-meta-mtime` as floating point since the epoch, accurate to 1 ns.

If the modification time needs to be updated rclone will attempt to perform a server
side copy to update the modification if the object can be copied in a single part.
In the case the object is larger than 5Gb, the object will be uploaded rather than copied.

Note that reading this from the object takes an additional `HEAD` request as the metadata
isn't returned in object listings.

### Multipart uploads

rclone supports multipart uploads with OOS which means that it can
upload files bigger than 5 GiB.

Note that files uploaded *both* with multipart upload *and* through
crypt remotes do not have MD5 sums.

rclone switches from single part uploads to multipart uploads at the
point specified by `--oos-upload-cutoff`.  This can be a maximum of 5 GiB
and a minimum of 0 (ie always upload multipart files).

The chunk sizes used in the multipart upload are specified by
`--oos-chunk-size` and the number of chunks uploaded concurrently is
specified by `--oos-upload-concurrency`.

Multipart uploads will use `--transfers` * `--oos-upload-concurrency` *
`--oos-chunk-size` extra memory.  Single part uploads to not use extra
memory.

Single part transfers can be faster than multipart transfers or slower
depending on your latency from oos - the more latency, the more likely
single part transfers will be faster.

Increasing `--oos-upload-concurrency` will increase throughput (8 would
be a sensible value) and increasing `--oos-chunk-size` also increases
throughput (16M would be sensible).  Increasing either of these will
use more memory.  The default values are high enough to gain most of
the possible performance without using too much memory.

{{< rem autogenerated options start" - DO NOT EDIT - instead edit fs.RegInfo in backend/oracleobjectstorage/oracleobjectstorage.go then run make backenddocs" >}}
### Standard options

Here are the Standard options specific to oracleobjectstorage (Oracle Cloud Infrastructure Object Storage).

#### --oos-provider

Choose your Auth Provider

Properties:

- Config:      provider
- Env Var:     RCLONE_OOS_PROVIDER
- Type:        string
- Default:     "env_auth"
- Examples:
    - "env_auth"
        - automatically pickup the credentials from runtime(env), first one to provide auth wins
    - "user_principal_auth"
        - use an OCI user and an API key for authentication.
        - you’ll need to put in a config file your tenancy OCID, user OCID, region, the path, fingerprint to an API key.
        - https://docs.oracle.com/en-us/iaas/Content/API/Concepts/sdkconfig.htm
    - "instance_principal_auth"
        - use instance principals to authorize an instance to make API calls. 
        - each instance has its own identity, and authenticates using the certificates that are read from instance metadata. 
        - https://docs.oracle.com/en-us/iaas/Content/Identity/Tasks/callingservicesfrominstances.htm
    - "resource_principal_auth"
        - use resource principals to make API calls
    - "no_auth"
        - no credentials needed, this is typically for reading public buckets

#### --oos-namespace

Object storage namespace

Properties:

- Config:      namespace
- Env Var:     RCLONE_OOS_NAMESPACE
- Type:        string
- Required:    true

#### --oos-compartment

Object storage compartment OCID

Properties:

- Config:      compartment
- Env Var:     RCLONE_OOS_COMPARTMENT
- Provider:    !no_auth
- Type:        string
- Required:    true

#### --oos-region

Object storage Region

Properties:

- Config:      region
- Env Var:     RCLONE_OOS_REGION
- Type:        string
- Required:    true

#### --oos-endpoint

Endpoint for Object storage API.

Leave blank to use the default endpoint for the region.

Properties:

- Config:      endpoint
- Env Var:     RCLONE_OOS_ENDPOINT
- Type:        string
- Required:    false

#### --oos-config-file

Path to OCI config file

Properties:

- Config:      config_file
- Env Var:     RCLONE_OOS_CONFIG_FILE
- Provider:    user_principal_auth
- Type:        string
- Default:     "~/.oci/config"
- Examples:
    - "~/.oci/config"
        - oci configuration file location

#### --oos-config-profile

Profile name inside the oci config file

Properties:

- Config:      config_profile
- Env Var:     RCLONE_OOS_CONFIG_PROFILE
- Provider:    user_principal_auth
- Type:        string
- Default:     "Default"
- Examples:
    - "Default"
        - Use the default profile

### Advanced options

Here are the Advanced options specific to oracleobjectstorage (Oracle Cloud Infrastructure Object Storage).

#### --oos-storage-tier

The storage class to use when storing new objects in storage. https://docs.oracle.com/en-us/iaas/Content/Object/Concepts/understandingstoragetiers.htm

Properties:

- Config:      storage_tier
- Env Var:     RCLONE_OOS_STORAGE_TIER
- Type:        string
- Default:     "Standard"
- Examples:
    - "Standard"
        - Standard storage tier, this is the default tier
    - "InfrequentAccess"
        - InfrequentAccess storage tier
    - "Archive"
        - Archive storage tier

#### --oos-upload-cutoff

Cutoff for switching to chunked upload.

Any files larger than this will be uploaded in chunks of chunk_size.
The minimum is 0 and the maximum is 5 GiB.

Properties:

- Config:      upload_cutoff
- Env Var:     RCLONE_OOS_UPLOAD_CUTOFF
- Type:        SizeSuffix
- Default:     200Mi

#### --oos-chunk-size

Chunk size to use for uploading.

When uploading files larger than upload_cutoff or files with unknown
size (e.g. from "rclone rcat" or uploaded with "rclone mount" or google
photos or google docs) they will be uploaded as multipart uploads
using this chunk size.

Note that "upload_concurrency" chunks of this size are buffered
in memory per transfer.

If you are transferring large files over high-speed links and you have
enough memory, then increasing this will speed up the transfers.

Rclone will automatically increase the chunk size when uploading a
large file of known size to stay below the 10,000 chunks limit.

Files of unknown size are uploaded with the configured
chunk_size. Since the default chunk size is 5 MiB and there can be at
most 10,000 chunks, this means that by default the maximum size of
a file you can stream upload is 48 GiB.  If you wish to stream upload
larger files then you will need to increase chunk_size.

Increasing the chunk size decreases the accuracy of the progress
statistics displayed with "-P" flag.


Properties:

- Config:      chunk_size
- Env Var:     RCLONE_OOS_CHUNK_SIZE
- Type:        SizeSuffix
- Default:     5Mi

#### --oos-upload-concurrency

Concurrency for multipart uploads.

This is the number of chunks of the same file that are uploaded
concurrently.

If you are uploading small numbers of large files over high-speed links
and these uploads do not fully utilize your bandwidth, then increasing
this may help to speed up the transfers.

Properties:

- Config:      upload_concurrency
- Env Var:     RCLONE_OOS_UPLOAD_CONCURRENCY
- Type:        int
- Default:     10

#### --oos-copy-cutoff

Cutoff for switching to multipart copy.

Any files larger than this that need to be server-side copied will be
copied in chunks of this size.

The minimum is 0 and the maximum is 5 GiB.

Properties:

- Config:      copy_cutoff
- Env Var:     RCLONE_OOS_COPY_CUTOFF
- Type:        SizeSuffix
- Default:     4.656Gi

#### --oos-copy-timeout

Timeout for copy.

Copy is an asynchronous operation, specify timeout to wait for copy to succeed


Properties:

- Config:      copy_timeout
- Env Var:     RCLONE_OOS_COPY_TIMEOUT
- Type:        Duration
- Default:     1m0s

#### --oos-disable-checksum

Don't store MD5 checksum with object metadata.

Normally rclone will calculate the MD5 checksum of the input before
uploading it so it can add it to metadata on the object. This is great
for data integrity checking but can cause long delays for large files
to start uploading.

Properties:

- Config:      disable_checksum
- Env Var:     RCLONE_OOS_DISABLE_CHECKSUM
- Type:        bool
- Default:     false

#### --oos-encoding

The encoding for the backend.

See the [encoding section in the overview](/overview/#encoding) for more info.

Properties:

- Config:      encoding
- Env Var:     RCLONE_OOS_ENCODING
- Type:        MultiEncoder
- Default:     Slash,InvalidUtf8,Dot

#### --oos-leave-parts-on-error

If true avoid calling abort upload on a failure, leaving all successfully uploaded parts on S3 for manual recovery.

It should be set to true for resuming uploads across different sessions.

WARNING: Storing parts of an incomplete multipart upload counts towards space usage on object storage and will add
additional costs if not cleaned up.


Properties:

- Config:      leave_parts_on_error
- Env Var:     RCLONE_OOS_LEAVE_PARTS_ON_ERROR
- Type:        bool
- Default:     false

#### --oos-no-check-bucket

If set, don't attempt to check the bucket exists or create it.

This can be useful when trying to minimise the number of transactions
rclone does if you know the bucket exists already.

It can also be needed if the user you are using does not have bucket
creation permissions.


Properties:

- Config:      no_check_bucket
- Env Var:     RCLONE_OOS_NO_CHECK_BUCKET
- Type:        bool
- Default:     false

#### --oos-sse-customer-key-file

To use SSE-C, a file containing the base64-encoded string of the AES-256 encryption key associated
with the object. Please note only one of sse_customer_key_file|sse_customer_key|sse_kms_key_id is needed.'

Properties:

- Config:      sse_customer_key_file
- Env Var:     RCLONE_OOS_SSE_CUSTOMER_KEY_FILE
- Type:        string
- Required:    false
- Examples:
    - ""
        - None

#### --oos-sse-customer-key

To use SSE-C, the optional header that specifies the base64-encoded 256-bit encryption key to use to
encrypt or  decrypt the data. Please note only one of sse_customer_key_file|sse_customer_key|sse_kms_key_id is
needed. For more information, see Using Your Own Keys for Server-Side Encryption 
(https://docs.cloud.oracle.com/Content/Object/Tasks/usingyourencryptionkeys.htm)

Properties:

- Config:      sse_customer_key
- Env Var:     RCLONE_OOS_SSE_CUSTOMER_KEY
- Type:        string
- Required:    false
- Examples:
    - ""
        - None

#### --oos-sse-customer-key-sha256

If using SSE-C, The optional header that specifies the base64-encoded SHA256 hash of the encryption
key. This value is used to check the integrity of the encryption key. see Using Your Own Keys for 
Server-Side Encryption (https://docs.cloud.oracle.com/Content/Object/Tasks/usingyourencryptionkeys.htm).

Properties:

- Config:      sse_customer_key_sha256
- Env Var:     RCLONE_OOS_SSE_CUSTOMER_KEY_SHA256
- Type:        string
- Required:    false
- Examples:
    - ""
        - None

#### --oos-sse-kms-key-id

if using using your own master key in vault, this header specifies the 
OCID (https://docs.cloud.oracle.com/Content/General/Concepts/identifiers.htm) of a master encryption key used to call
the Key Management service to generate a data encryption key or to encrypt or decrypt a data encryption key.
Please note only one of sse_customer_key_file|sse_customer_key|sse_kms_key_id is needed.

Properties:

- Config:      sse_kms_key_id
- Env Var:     RCLONE_OOS_SSE_KMS_KEY_ID
- Type:        string
- Required:    false
- Examples:
    - ""
        - None

#### --oos-sse-customer-algorithm

If using SSE-C, the optional header that specifies "AES256" as the encryption algorithm.
Object Storage supports "AES256" as the encryption algorithm. For more information, see
Using Your Own Keys for Server-Side Encryption (https://docs.cloud.oracle.com/Content/Object/Tasks/usingyourencryptionkeys.htm).

Properties:

- Config:      sse_customer_algorithm
- Env Var:     RCLONE_OOS_SSE_CUSTOMER_ALGORITHM
- Type:        string
- Required:    false
- Examples:
    - ""
        - None
    - "AES256"
        - AES256

## Backend commands

Here are the commands specific to the oracleobjectstorage backend.

Run them with

    rclone backend COMMAND remote:

The help below will explain what arguments each command takes.

See the [backend](/commands/rclone_backend/) command for more
info on how to pass options and arguments.

These can be run on a running backend using the rc command
[backend/command](/rc/#backend-command).

### rename

change the name of an object

    rclone backend rename remote: [options] [<arguments>+]

This command can be used to rename a object.

Usage Examples:

    rclone backend rename oos:bucket relative-object-path-under-bucket object-new-name


### list-multipart-uploads

List the unfinished multipart uploads

    rclone backend list-multipart-uploads remote: [options] [<arguments>+]

This command lists the unfinished multipart uploads in JSON format.

    rclone backend list-multipart-uploads oos:bucket/path/to/object

It returns a dictionary of buckets with values as lists of unfinished
multipart uploads.

You can call it with no bucket in which case it lists all bucket, with
a bucket or with a bucket and path.

    {
      "test-bucket": [
                {
                        "namespace": "test-namespace",
                        "bucket": "test-bucket",
                        "object": "600m.bin",
                        "uploadId": "51dd8114-52a4-b2f2-c42f-5291f05eb3c8",
                        "timeCreated": "2022-07-29T06:21:16.595Z",
                        "storageTier": "Standard"
                }
        ]


### cleanup

Remove unfinished multipart uploads.

    rclone backend cleanup remote: [options] [<arguments>+]

This command removes unfinished multipart uploads of age greater than
max-age which defaults to 24 hours.

Note that you can use --interactive/-i or --dry-run with this command to see what
it would do.

    rclone backend cleanup oos:bucket/path/to/object
    rclone backend cleanup -o max-age=7w oos:bucket/path/to/object

Durations are parsed as per the rest of rclone, 2h, 7d, 7w etc.


Options:

- "max-age": Max age of upload to delete

{{< rem autogenerated options stop >}}
