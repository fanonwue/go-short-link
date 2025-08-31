# Development

This section is meant for potential contributors and will explain how to extend certain parts of the software.

## Architectural overview

The application is structured around a small set of responsibilities. An HTTP layer that
serves incoming requests, a state layer that owns the live redirect mapping and exposes safe read access using mutexes,
and an update layer that refreshes the mapping at runtime without restarts. This separation allows the service to handle traffic efficiently 
while continuously ingesting updates in the background.

At the boundary, the HTTP server receives requests such as `GET /abc`, parses the short path component, and looks up the 
corresponding target using the shared state. The state component provides a thread-safe view of a map from string keys 
to string targets. Read access is protected by a read/write mutex, which allows many concurrent readers while ensuring 
that occasional writes during updates remain consistent. If a target exists for the requested key, the handler issues 
an HTTP redirect, typically using a temporary or permanent status depending on configuration; if there is no mapping, 
it responds with a 404. This lookup is intentionally simple and fast, so the majority of CPU time is spent serving traffic
rather than coordinating state.

The state layer, found in the package `/internal/state`, is designed to decouple request handling from update logic. 
It maintains the current mapping, exposes simple accessors, and manages two buffered channels: one for receiving new 
mappings and one for receiving errors produced during update operations.
Dedicated goroutines listen to these channels. When a new mapping arrives, the listener replaces
the current map under a write lock and logs the resulting size for visibility. When an error is received, the state captures
it as the last-known error, making it available for diagnostics and health checks. Because channels are buffered and updates
are applied atomically, the system avoids partial states and minimizes the chance of producers blocking on consumers during
bursts.

Before a new mapping becomes active, the design allows for hook functions that transform or validate the data. Hooks can
normalize keys, reject malformed targets, enforce policy, or augment entries. This makes the state layer an extensibility
point for enforcing operational guarantees without complicating the HTTP handlers. The ability to retrieve a copy of the 
current mapping enables safe iteration for admin views or debugging without risking mutation of the shared state.

In operation, a background updater periodically (or on demand, when using the [API](#forcing-a-redirect-mapping-update)) 
fetches the desired mapping from a source such as a file, 
API, or database. It applies hooks to sanitize or validate the content and then publishes the processed map to the state 
via the update channel, while forwarding any errors to the error channel. The HTTP layer continues serving requests 
throughout this process, always consulting the latest successfully applied map. If an update fails, the service retains 
the last good state and surfaces the failure through the recorded last error, ensuring resilience in the face of 
transient data source issues.

From a performance and concurrency perspective, this approach favors read-heavy workloads. The read/write mutex allows 
many concurrent lookups with minimal contention, while writes occur only during updates and are short-lived. The use of 
buffered channels and goroutine listeners isolates update cadence from request throughput and avoids blocking request
handlers on I/O or validation. Copy-on-read for diagnostic access prevents accidental mutation of shared state.

Operationally, the application lends itself to stateless deployment. The process runs an HTTP server and one or more 
background updaters. Health and readiness probes can distinguish between liveness (server running) and readiness 
(initial mapping loaded and no critical errors). On shutdown, updaters can stop, channels close, and in-flight requests 
drain gracefully. Administrators may expose optional endpoints to introspect the current mapping, view the last error, 
or trigger a refresh, though the core redirect path remains independent of these features.


## Code Docs

An automatically generated documentation of the code can be found on the [Go package mirror](https://pkg.go.dev/github.com/fanonwue/go-short-link).
Please note that most of the implementation is within the `internal` package, which is not included in the documentation by default.
You need to explicitly enable it by clicking the "Show internal" buttons. Alternatively, use this 
[link to the package mirror](https://pkg.go.dev/github.com/fanonwue/go-short-link/internal) to
directly view the internal package.

## Creating a new RedirectDataSource

While the default data source is based on Google Spreadsheets, it's possible to implement your own data source. The
application expects a struct that implements the `RedirectDataSource` interface found in the `internal/ds` package.

```{literalinclude} ../internal/ds/data-source.go
:language: go
```


The interface consists of five methods that need to be implemented. The most important one is `FetchRedirectMapping()`,
as it's result will be used to create the internal redirect mapping structure. Implementing `NeedsUpdate()` can reduce
possibly expensive calls to storage providers. An update will only be performed if this method returns true.
The remaining functions can be either used to implement other functions, or can be used by external programs utilizing
this application's API.

To demonstrate the implementation of a custom data source, an example implementation of a CSV-based provider can be seen
in the following code block (it can be found in the `internal/ds/csv.go` file as well).
It utilizes Go's native CSV library to handle reading the records, and will use appropriate
filesystem operations to implement `NeedsUpdate()` and `LastModified()`. When working with file-based providers, special
care needs to be taken to always close created file handles. Go's `defer` functionality is very useful to handle
this. Accessing the CSV-file through the `withFile()` handler ensures that the file handle will always be closed.

:::{collapse} Example implementation of a CSV-based data source
:class: collapse
```{literalinclude} ../internal/ds/csv.go
:language: go
```
:::
<br>

## Using the new data source

The application does not support configurable data sources at this point. To use your newly created data source, you
will need to modify the `Setup()` function inside `internal/repo/repo.go`. It should be enough to simply replace the
call to `ds.CreateSheetsDataSource()` with your newly created implementation.

## Hooks

As mentioned in the architecture overview, the state layer allows for hook functions to be registered. These functions
are called during updates and can be used to normalize, validate, or augment the data. The following example shows
how to implement a hook that normalizes keys to lowercase.

```go
// Add a hook that normalizes keys to lowercase
// mapState is a reference to the current state.RedirectMapState:
// var mapState state.RedirectMapState

mapState.AddHook(func(originalMap state.RedirectMap) state.RedirectMap {
    // Edit map in place
    for key := range originalMap {
        modifyKey(originalMap, key, func(s string) string {
            return strings.ToLower(s)
        })
    }
    return originalMap
})
```

Once a new redirect mapping has been applied, the registered hook functions will be called in the order they were added.

## Extending the API

It's easy to extend the API to support additional endpoints. Endpoints are defined in the `internal/api` package. The basis
of API endpoints is the `Endpoint` struct, which defines a handler function and the pattern that matches the endpoint.
To add a new endpoint, simply create a new instance of the `Endpoint` struct and add it the `endpoints` slice found in
the  `createEndpoints()` function. Appropriate middleware will be applied to the handler function, and all endpoints
will be registered with the HTTP server automatically.
By default, Endpoints require authentication, but this can be disabled by setting the `Anonymous` field to `true` in the `Endpoint` struct.

The `Endpoint` struct is defined as follows:
```go
type Endpoint struct {
    // Pattern is the URL pattern that this endpoint matches. The matching will be done by the HTTP server.
    Pattern string
    // Handler the handler that can handle the incoming request
    Handler http.HandlerFunc
    // Anonymous specifies whether anonymous (unauthenticated) access to this endpoint is allowed
    Anonymous bool
}
```

The handler function is a standard `http.HandlerFunc` provided by the standard library that takes a `http.ResponseWriter` and `*http.Request` as arguments.
The `*http.Request` object contains the parsed URL path and query parameters, and the `http.ResponseWriter` can be used to
write the response body.

To respond to the request, the `internal/srv` package provides a set of helper functions that can be used to write the response, such
as `srv.TextResponse()` for raw text responses or `srv.JsonResponse()` for responses that should be marshaled to JSON. If
the response is going to be marshaled to JSON, it needs to be compatible with the 
standard library's `encoding/json` [package](https://pkg.go.dev/encoding/json). Please be advised that an upgrade to the
`encoding/json/v2` [package](https://pkg.go.dev/encoding/json/v2) is planned once it's a stable part of the Go standard library.