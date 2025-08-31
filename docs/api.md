# API

The application provides a small API for use with monitoring software and integration with other services.

Endpoints that start with `/_api/` are only available if `APP_ENABLE_API` is set to true. 
Likewise, the `/_status/` endpoints are only available if `APP_DISABLE_STATUS` is set to false.
It is recommended to make sure access control is enabled by setting a username and password in `APP_ADMIN_USER` and `APP_ADMIN_PASS` respectively.
Accessing protected endpoints without authentication when access control is enabled will result in a `401 Unauthorized` status code.
Trying to use the wrong HTTP method will result in a `405 Method Not Allowed` status code.

For more information on configuration settings related to the API or status endpoints, see [](#configuration-table).

(health-check)=
## Health check

This endpoint allows you to check the health of the application. It does not require authentication since no sensitive information is returned.

| Method | Path              | Description                                                      | Protected |
|--------|-------------------|------------------------------------------------------------------|-----------|
| `GET`  | `/_status/health` | Returns non-sensitive information about the application's health | No        |
| `GET`  | `/_api/health`    | Returns non-sensitive information about the application's health | No        |

The response will be a JSON object that conforms to the following example:
```json
{
  "mappingSize": 10,
  "running": true,
  "healthy": true,
  "lastUpdate": "2025-01-31T12:00:00.000Z"
}
```

(state-information)=
## State information

This endpoint returns information about the application's state. Information includes:

- The current mapping in a key-value format
- The last time the mapping was updated
- The last time the data source has been modified
- The ID of the currently used spreadsheet (this might change in the future to enable different forms of data sources)

When access control is enabled, this endpoint requires HTTP Basic Auth.

| Method | Path            | Description                                                                            | Protected            |
|--------|-----------------|:---------------------------------------------------------------------------------------|----------------------|
| `GET`  | `/_status/info` | Returns information about the current state,<br>including the currently active mapping | Yes, HTTP Basic Auth |
| `GET`  | `/_api/info`    | Returns information about the current state,<br>including the currently active mapping | Yes, HTTP Basic Auth |

The response will be a JSON object that conforms to the following example:
```json
{
  "mapping": {
    "__root": "https://example.com/shortlink",
    "example": "https://example.com/example",
    "new-example": "https://github.com"
  },
  "spreadsheetId": "1234567890",
  "lastUpdate": "2025-01-31T12:00:00.000Z",
  "lastModified": "2025-01-01T14:00:00.000Z",
  "lastError": "Error message" // Only present if an error occurred during the last update
}
```

(forcing-a-redirect-mapping-update)=
## Forcing a redirect mapping update

This endpoint allows you to forcefully refresh the redirect mapping. When access control is enabled, this endpoint requires HTTP Basic Auth.

| Method        | Path                   | Description                               | Protected            |
|---------------|------------------------|-------------------------------------------|----------------------|
| `GET`, `POST` | `/_api/update-mapping` | Forcefully refreshes the redirect mapping | Yes, HTTP Basic Auth |

If successful, the API responds with a `200 OK` status code and a small, accompanying text, informing
the caller about the new mapping size. 
On failure, the API responds with a `500 Internal Server Error` status code and will write the error into the response body as text.

Please note that this endpoint might change in the future to return an appropriate JSON response.