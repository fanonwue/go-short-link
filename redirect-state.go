package main

import "sync"

type (
	// RedirectMap is a map of string keys and string values. The key is meant to be interpreted as the redirect path,
	// which has been provided by the user, while the value represents the redirect target (as in, where the redirect
	// should lead to).
	RedirectMap = map[string]string

	// RedirectMapHook A function that takes a RedirectMap, processes it and returns a new RedirectMap with
	// the processed result.
	RedirectMapHook = func(RedirectMap) RedirectMap

	RedirectMapState struct {
		mapping RedirectMap
		hooks   []RedirectMapHook
		mutex   sync.RWMutex
		channel chan RedirectMap
	}
)

func (state *RedirectMapState) UpdateMapping(newMap RedirectMap) {
	// Synchronize using a mutex to prevent race conditions
	state.mutex.Lock()
	// Defer unlock to make sure it always happens, regardless of panics etc.
	defer state.mutex.Unlock()
	state.mapping = newMap
}

func (state *RedirectMapState) GetTarget(key string) (string, bool) {
	// Synchronize using a mutex to prevent race conditions
	state.mutex.RLock()
	// Defer unlock to make sure it always happens, regardless of panics etc.
	defer state.mutex.RUnlock()
	target, ok := state.mapping[key]
	return target, ok
}

// CurrentMapping creates a copy of the current mapping and returns the copied map.
func (state *RedirectMapState) CurrentMapping() RedirectMap {
	state.mutex.RLock()
	defer state.mutex.RUnlock()
	targetMap := make(RedirectMap, len(state.mapping))

	for key, value := range state.mapping {
		targetMap[key] = value
	}

	return targetMap
}

func (state *RedirectMapState) MappingSize() int {
	state.mutex.RLock()
	defer state.mutex.RUnlock()
	return len(state.mapping)
}

func (state *RedirectMapState) Hooks() []RedirectMapHook {
	return state.hooks
}

func (state *RedirectMapState) AddHook(hook RedirectMapHook) {
	newHooks := append(state.hooks, hook)
	state.hooks = newHooks
}

func (state *RedirectMapState) Channel() chan<- RedirectMap {
	if state.channel == nil {
		state.channel = make(chan RedirectMap)
	}
	return state.channel
}

func (state *RedirectMapState) ListenForUpdates() chan<- RedirectMap {
	channel := state.Channel()
	go state.updateListener()
	return channel
}

func (state *RedirectMapState) updateListener() {
	for mapping := range state.channel {
		state.UpdateMapping(mapping)
		logger.Infof("Updated redirect mapping, number of entries: %d", len(mapping))
	}

	// The channel has been closed at this point, so we need to reflect that in our local state
	logger.Debugf("Setting redirect state channel to nil")
	state.channel = nil
}
