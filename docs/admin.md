# Administration


This part of the documentation is meant for administrators and users of this software. It will explain how to configure
the software to run on different kinds of system configurations.

(installation)=
## Installation

The recommended way to install this server is by running it inside a Docker container. Pre-built images are provided for
Linux-based systems running `amd64`, `aarch64` and `rsicv` architectures. They are available from the GitHub Container
Registry and can be pulled via the following command:

```console
docker pull ghcr.io/fanonwue/go-short-link:master
```

However, it's recommended to set up a `compose.yml` for use with Docker Compose.

```yaml
services:
  app:
    image: ghcr.io/fanonwue/go-short-link:master
    restart: always
    env_file:
      - .env
    volumes:
      - ./data/fallback.json:/opt/app/fallback.json
      - ./secret:/opt/app/secret:ro
```

The application is configured by using environment variables. You can replace the `env_file` directive as seen in the above
compose file with an `environment` block if you do not wish to use an `.env` file. Please refer to the [configuration](#configuration)
section for further details on which options are supported.

## Building from source

In case you do not wish to run this application using Docker, it's possible to build it yourself. Please ensure that
the following prerequisites are installed on your system:

* [Go toolchain](https://go.dev/dl/), version 1.25 or newer
* Make (optional, but the example commands will make use of it)

To start, pull the repository using the following command:

```console
git pull git@github.com:fanonwue/go-short-link.git
```

The next step involves actually building the application. To simplify the build process, a Makefile is provided. For a
standard build, simply execute the command

```console
make build
```

and the compiled binary will be present in the `bin/` directory.

To customize the build, there are several environment variables you can set. Setting `TARGET=prod` will produce a binary
without any debug information and with stripped symbols, which will be significantly smaller. The Go toolchain itself also
provides useful options. If your system has C build tools installed (like `cc` or `clang`), the binary will not be statically
linked, but use dynamic linking to link to your installed `libc` library. You can disable this behaviour by setting
`CGO_ENABLED=0`. This will produce a statically linked binary. If you wish to compile for a different platform, for example
Windows, you can make use of the `GOOS` and `GOARCH` environment variables.

To build a standalone, statically linked and stripped binary, you could use this command:


```console
CGO_ENABLED=0 TARGET=prod make build
```

To compile for Windows while running on a Linux-based system, this command can be used instead:


```console
GOOS=win CGO_ENABLED=0 TARGET=prod make build
```

The resulting binary in the `bin/` directory will be suffixed using the common `.exe` file extension common on Windows-based systems.

(configuration)=
## Configuration

The software can be configured using environment variables. Please refer to the [configuration](#configuration-table)
table for a list of all supported options.

(configuration-table)=
:::{table} Configuration variables
:widths: auto
:align: center
:class: multi-line-table

| Variable                 | Default               | Description                                                                                                                                                                                                                     |
|--------------------------|-----------------------|---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| APP_PORT                 | 3000                  | The port the server will listen on.                                                                                                                                                                                             |
| APP_UPDATE_PERIOD        | 300                   | The period (in seconds) between updates.                                                                                                                                                                                        |
| APP_ENABLE_API           | false                 | Whether to enable the API endpoints underneath the `/_api/` path.                                                                                                                                                               |
| APP_ADMIN_USER           | ""                    | The username needed to access sensitive API and status endpoints.                                                                                                                                                               |
| APP_ADMIN_PASS           | ""                    | The password needed to access sensitive API and status endpoints.                                                                                                                                                               |
| APP_FAVICON              | ""                    | A comma separated list of favicons to include in non-redirect responses. If your redirections contain a redirect for `/favicon.ico`, you could set this value to `favicon.ico`. The client will be redirected to the real icon. |
| APP_ALLOW_ROOT_REDIRECT  | true                  | Whether to allow redirects without any given path. See [](#special-redirection-names-table).                                                                                                                                    |
| APP_IGNORE_CASE_IN_PATH  | true                  | If true, redirection names are handled in a case-insensitive manner.                                                                                                                                                            |
| APP_ENABLE_REDIRECT_BODY | true                  | If true, a stub body will be generated when sending the redirection response, notifying the user of a redirection in case the browser does not honor the header.                                                                |
| APP_HTTP_CACHE_MAX_AGE   | APP_UPDATE_PERIOD * 2 | The duration (in seconds) in which the response shall be cached by the client.                                                                                                                                                  |
| APP_SHOW_SERVER_HEADER   | true                  | If true, the `Server` header in the response will be set to `go-short-link`. It will not be set at all otherwise.                                                                                                               |
| APP_ENABLE_ETAG          | true                  | Whether to generate an Etag value for the response header.                                                                                                                                                                      |
| APP_SHOW_REPOSITORY_LINK | false                 | If true, non-redirect responses will contain a link to the GitHub repository.                                                                                                                                                   |
| APP_DISABLE_STATUS       | false                 | If true, the endpoints underneath the `/_status/` path will be disabled.                                                                                                                                                        |
| APP_FALLBACK_FILE        | ""                    | If set, a fallback file will be created at the specified path. If the server restarts and is unable to fetch a redirect mapping from the provider, that file will be loaded instead, containing the last known-good state.      |
:::


## Configuring Google Spreadsheets

The default data source for this link shortener is a Google Spreadsheets document. This provides several administrative
advantages, such as:

* Easy management of redirection mappings
* Shareable management access
* TODO

The server utilizes Google's APIs for access, therefore appropriate access has to be granted. This is possible by using
the Google Cloud Console.

Visit the [Google Cloud Console](https://console.cloud.google.com) and create a new project. Under `APIs and services`, add the `Google Sheets API`. This
API is mandatory. The `Google Drive API` is optional, and allows checking the last modification time of the spreadsheet.
Enabling it allows the server to skip querying the contents of the spreadsheet if it hasn't changed since the last retrieval.

To use the API, credentials are needed. An easy way to gain access to the Google API is by use of service accounts. Within
the `APIs and Services` section of the cloud console, navigate to the `Credentials` tab. This allows you to create new
service account credentials. Create a new service account with a name you prefer. It is not necessary to grant this
service account any permissions, as we will explicitly grant access to the spreadsheet later. Once created, the service
account will require setting up access keys. Open the configuration page of the newly created service account and create
a key. Select the `JavaScript Object Notation` option and continue. Your browser will ask you to save the file somewhere.
The file contains all necessary information to access the API.

The application requires three values from the previously saved file:

* the client email (`client_email`)
* the private key (`private_key`)
* the private key ID (`private_key_id`)

You can set the client email and the private key ID via the environment variables `APP_SERVICE_ACCOUNT_CLIENT_EMAIL` as well
as `APP_SERVICE_ACCOUNT_PRIVATE_KEY_ID` respectively. The private key itself can be set via the environment variable
`APP_SERVICE_ACCOUNT_PRIVATE_KEY` too (keeping the escaped line breaks `\n` within the string), but this is discouraged.
Instead, you can place your private key into a secret file located at `./secret/privateKey.pem` (or whatever you configure
using the `SERVICE_ACCOUNT_PRIVATE_KEY_FILE` environmnent variable). The escaped line breaks (`\n`) can be replaced with actual
Unix-style line breaks, but the application will handle this as well if this step has not been taken.

Finally, you need to create a properly formatted spreadsheet. The server expects a table that is formatted like the one
shown in the following table.


| Redirection Name | Target              | Optional Column                  | ...                     |
|------------------|---------------------|----------------------------------|-------------------------|
| __root           | https://example.com | You could put a description here | and something else here |
| test-redirect    | https://github.com  |                                  |                         |



The first two columns are fixed, meaning the application expects column A to always be the redirection name, and column B
to always be the target. All other columns are not queried, and you can use them to add more information, like a description
or an automatically generated, copy-able link to those. For explanations on how the special names in the first column work, 
refer to the [special redirection names](#special-redirection-names) section.

As we are using a service account, you need to grant that service account access to the spreadsheet. This is done by simply
sharing the spreadsheet. Your created service account has an email attached to it, which should look similar to
`<service-account-name>@<project-name-and-id>.iam.gserviceaccount.com`. Using Google Spreadsheet's sharing function, you can
just share the document to the service account using it's email address. Read-only access is enough.

## Using a fallback file



(special-redirection-names)=
## Special redirection names

The application supports several different, keyword-like special redirection names. Refer to the following
[](#special-redirection-names-table) for an overview. In the examples, assume that the server is available via `https://redirect.example.com`.

(special-redirection-names-table)=
:::{table} Special redirection names
:widths: auto
:align: center
:class: multi-line-table

| Redirection Name | Meaning                                                                                                                                                                                                                                       |
|------------------|-----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| __root           | When calling the server with an empty path, this redirection will be used. In this example, calling `https://redirect.example.com` would trigger this redirection                                                                             |
| test-redirect    | When the request's hostname matches a record and the path is empty, this redirection will be triggered. Assuming \<hostname\> is replaced with `https://redirect2.example.com`, calling that URL will trigger this record instead of `__root` |
:::

adsad
