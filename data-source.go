package main

import (
	"time"
)

type RedirectDataSource interface {
	// LastUpdate returns the timestamp at which the last update occurred
	LastUpdate() *time.Time
	// LastModified returns the timestamp at which the data source has been modified
	LastModified() *time.Time
	// NeedsUpdate returns true when the data source provider determined that an update of the redirect mapping is necessary
	NeedsUpdate() bool
	// FetchRedirectMapping returns the current redirect mapping from the provider
	FetchRedirectMapping() (RedirectMap, error)
	// Id returns a provider specific identifier
	Id() string
}
