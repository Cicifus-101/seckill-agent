package tool

import "fmt"

type Registry struct {
	items map[string]Tool
}

func NewRegistry() *Registry {
	return &Registry{
		items: make(map[string]Tool),
	}
}

func (r *Registry) Register(t Tool) {
	r.items[t.Info().Name] = t
}

func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.items[name]
	return t, ok
}

func (r *Registry) List() []Info {
	res := make([]Info, 0, len(r.items))
	for _, t := range r.items {
		res = append(res, t.Info())
	}
	return res
}

func (r *Registry) MustGet(name string) Tool {
	t, ok := r.Get(name)
	if !ok {
		panic(fmt.Sprintf("tool not found: %s", name))
	}
	return t
}
