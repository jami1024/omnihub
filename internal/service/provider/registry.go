package provider

import (
	"fmt"
	"sort"
	"sync"
)

// Registry holds the active set of Drivers indexed by their Name(). It
// is the lookup table the Forwarder Guard consults to find the driver
// for a resolved Account.
//
// Registry is safe for concurrent use. Registration typically happens
// once at startup; lookups happen on every request.
type Registry struct {
	mu      sync.RWMutex
	drivers map[string]Driver
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{drivers: make(map[string]Driver)}
}

// Register adds d to the registry under its Name(). It returns an
// error if a driver with the same name is already registered or if
// d.Name() is empty.
func (r *Registry) Register(d Driver) error {
	if d == nil {
		return fmt.Errorf("provider.Registry: nil driver")
	}
	name := d.Name()
	if name == "" {
		return fmt.Errorf("provider.Registry: driver has empty Name()")
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.drivers[name]; exists {
		return fmt.Errorf("provider.Registry: driver %q already registered", name)
	}
	r.drivers[name] = d
	return nil
}

// MustRegister is like Register but panics on error. Use it at
// init/wire time when the registration target is known and a failure
// indicates a programmer error.
func (r *Registry) MustRegister(d Driver) {
	if err := r.Register(d); err != nil {
		panic(err)
	}
}

// Get returns the driver registered under name, or (nil, false) if no
// such driver exists.
func (r *Registry) Get(name string) (Driver, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	d, ok := r.drivers[name]
	return d, ok
}

// Names returns the sorted list of registered driver names.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.drivers))
	for n := range r.drivers {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
