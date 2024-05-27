package state

import (
	"github.com/fanonwue/go-short-link/internal/util"
	"sync"
)

type (
	// RedirectMap is a map of string keys and string values. The key is meant to be interpreted as the redirect path,
	// which has been provided by the user, while the value represents the redirect target (as in, where the redirect
	// should lead to).
	RedirectMap map[string]string

	// RedirectMapHook A function that takes a RedirectMap, processes it and returns a new RedirectMap with
	// the processed result.
	RedirectMapHook func(RedirectMap) RedirectMap

	RedirectMapState struct {
		mapping          RedirectMap
		hooks            []RedirectMapHook
		mappingMutex     sync.RWMutex
		mappingChannel   chan RedirectMap
		lastError        error
		lastErrorChannel chan error
		lastErrorMutex   sync.RWMutex
	}
)

func NewState() RedirectMapState {
	return RedirectMapState{
		mapping: RedirectMap{},
		hooks:   make([]RedirectMapHook, 0),
	}
}

func (state *RedirectMapState) LastError() error {
	state.lastErrorMutex.RLock()
	defer state.lastErrorMutex.RUnlock()
	return state.lastError
}

func (state *RedirectMapState) UpdateLastError(err error) {
	state.lastErrorMutex.Lock()
	defer state.lastErrorMutex.Unlock()
	state.lastError = err
}

func (state *RedirectMapState) UpdateMapping(newMap RedirectMap) {
	// Synchronize using a mappingMutex to prevent race conditions
	state.mappingMutex.Lock()
	// Defer unlock to make sure it always happens, regardless of panics etc.
	defer state.mappingMutex.Unlock()
	state.mapping = newMap
}

func (state *RedirectMapState) GetTarget(key string) (string, bool) {
	// Synchronize using a mappingMutex to prevent race conditions
	state.mappingMutex.RLock()
	// Defer unlock to make sure it always happens, regardless of panics etc.
	defer state.mappingMutex.RUnlock()
	target, ok := state.mapping[key]
	return target, ok
}

// CurrentMapping creates a copy of the current mapping and returns the copied map.
func (state *RedirectMapState) CurrentMapping() RedirectMap {
	state.mappingMutex.RLock()
	defer state.mappingMutex.RUnlock()
	targetMap := make(RedirectMap, len(state.mapping))

	for key, value := range state.mapping {
		targetMap[key] = value
	}

	return targetMap
}

func (state *RedirectMapState) MappingSize() int {
	state.mappingMutex.RLock()
	defer state.mappingMutex.RUnlock()
	return len(state.mapping)
}

func (state *RedirectMapState) Hooks() []RedirectMapHook {
	return state.hooks
}

func (state *RedirectMapState) AddHook(hook RedirectMapHook) {
	newHooks := append(state.hooks, hook)
	state.hooks = newHooks
}

func (state *RedirectMapState) MappingChannel() chan<- RedirectMap {
	if state.mappingChannel == nil {
		state.mappingChannel = make(chan RedirectMap)
	}
	return state.mappingChannel
}

func (state *RedirectMapState) ErrorChannel() chan<- error {
	if state.lastErrorChannel == nil {
		state.lastErrorChannel = make(chan error)
	}
	return state.lastErrorChannel
}

func (state *RedirectMapState) ListenForUpdateErrors() chan<- error {
	channel := state.ErrorChannel()
	go state.errorListener()
	return channel
}

func (state *RedirectMapState) ListenForUpdates() chan<- RedirectMap {
	channel := state.MappingChannel()
	go state.updateListener()
	return channel
}

func (state *RedirectMapState) updateListener() {
	for mapping := range state.mappingChannel {
		state.UpdateMapping(mapping)
		util.Logger().Infof("Updated redirect mapping, number of entries: %d", len(mapping))
	}

	// The mappingChannel has been closed at this point, so we need to reflect that in our local state
	util.Logger().Debugf("Setting redirect state mappingChannel to nil")
	state.mappingChannel = nil
}

func (state *RedirectMapState) errorListener() {
	for err := range state.lastErrorChannel {
		state.UpdateLastError(err)
	}

	util.Logger().Debugf("Setting redirect state errorChannel to nil")
	state.lastErrorChannel = nil
}
