package shell

// Router owns which surface is active and the cross-surface history.
// Within-surface drilldown belongs to the surface (Pop); the router only
// records surface switches, so back/forward is a navigation of places, not
// of every keystroke.
type Router struct {
	surfaces []Surface
	active   int
	back     []int
	fwd      []int
}

// NewRouter routes over surfaces; the first is active.
func NewRouter(surfaces []Surface) *Router {
	return &Router{surfaces: surfaces}
}

// Surfaces returns the surfaces in tab order.
func (r *Router) Surfaces() []Surface { return r.surfaces }

// Active returns the active surface.
func (r *Router) Active() Surface { return r.surfaces[r.active] }

// ActiveIndex returns the active surface's tab position.
func (r *Router) ActiveIndex() int { return r.active }

// Activate switches to surface i (no-op when out of range or already
// active), recording history.
func (r *Router) Activate(i int) bool {
	if i < 0 || i >= len(r.surfaces) || i == r.active {
		return false
	}
	r.back = append(r.back, r.active)
	r.fwd = nil
	r.active = i
	return true
}

// ActivateID switches to the surface with the given ID.
func (r *Router) ActivateID(id string) bool {
	for i, s := range r.surfaces {
		if s.ID() == id {
			return r.Activate(i)
		}
	}
	return false
}

// Back returns to the previously active surface.
func (r *Router) Back() bool {
	if len(r.back) == 0 {
		return false
	}
	r.fwd = append(r.fwd, r.active)
	r.active = r.back[len(r.back)-1]
	r.back = r.back[:len(r.back)-1]
	return true
}

// Forward re-enters a surface left via Back.
func (r *Router) Forward() bool {
	if len(r.fwd) == 0 {
		return false
	}
	r.back = append(r.back, r.active)
	r.active = r.fwd[len(r.fwd)-1]
	r.fwd = r.fwd[:len(r.fwd)-1]
	return true
}
