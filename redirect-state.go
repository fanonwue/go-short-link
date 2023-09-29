package main

import "sync"

type (
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
}
