package catalogresolve

// inherit applies the intent.yaml `catalog.defaults` layer underneath
// any explicit authored values, per resolution-pipeline.md §3:
//
//   - Scalar fields: lower wins ONLY when the higher layer left the
//     field unset (zero value).
//   - Map-valued fields (labels, annotations): each key is inherited
//     independently — explicit keys are preserved; missing keys fall
//     through.
//   - List-valued fields (tags, etc.): explicit lists win wholesale.
//     A list whose pointer is non-nil — including an explicit `[]` —
//     is "set"; only an absent (nil) list inherits.
//
// Provenance is updated for every field that was inherited, recording
// the intent file and the canonical JSON pointer ("/catalog/defaults/...")
// the value originated from.
//
// `intentRel` is the workspace-relative path of the intent file used
// for provenance attribution. When defaults is nil (no intent file or no
// catalog.defaults block) the manifest is returned unchanged.
func inherit(m AuthoredManifest, defaults *intentCatalogDefaults, intentRel string) AuthoredManifest {
	if defaults == nil {
		return m
	}
	if m.Provenance == nil {
		m.Provenance = map[string]Provenance{}
	}
	prov := func(field, ptr string) {
		m.Provenance[field] = Provenance{File: intentRel, Pointer: ptr}
	}

	// Scalars: spec.lifecycle, spec.owner, spec.system.
	if m.Component.Spec.Lifecycle == "" && defaults.Lifecycle != "" {
		m.Component.Spec.Lifecycle = defaults.Lifecycle
		prov("spec.lifecycle", "/catalog/defaults/lifecycle")
	}
	if m.Component.Spec.Owner == "" && defaults.Owner != "" {
		m.Component.Spec.Owner = defaults.Owner
		prov("spec.owner", "/catalog/defaults/owner")
	}
	if m.Component.Spec.System == "" && defaults.System != "" {
		m.Component.Spec.System = defaults.System
		prov("spec.system", "/catalog/defaults/system")
	}

	// Maps: per-key fill. Authored explicit-set keys remain authored;
	// missing keys come from defaults. nil authored map is fine — we
	// allocate on first inherited entry.
	if len(defaults.Labels) > 0 {
		for k, v := range defaults.Labels {
			if _, present := m.Component.Metadata.Labels[k]; present {
				continue
			}
			if m.Component.Metadata.Labels == nil {
				m.Component.Metadata.Labels = map[string]string{}
			}
			m.Component.Metadata.Labels[k] = v
			prov("metadata.labels."+k,
				"/catalog/defaults/labels/"+escapeJSONPointerToken(k))
		}
	}
	if len(defaults.Annotations) > 0 {
		for k, v := range defaults.Annotations {
			if _, present := m.Component.Metadata.Annotations[k]; present {
				continue
			}
			if m.Component.Metadata.Annotations == nil {
				m.Component.Metadata.Annotations = map[string]string{}
			}
			m.Component.Metadata.Annotations[k] = v
			prov("metadata.annotations."+k,
				"/catalog/defaults/annotations/"+escapeJSONPointerToken(k))
		}
	}

	// Lists: ComponentYAML in this package does not yet expose
	// metadata.tags, so defaults.Tags is intentionally a no-op (see
	// intent.go for the rationale). The function is structured so the
	// list-inheritance hook lands naturally when the model adds the
	// field — explicit-set vs unset is resolved on the consumer side
	// via the slice's nilness.

	return m
}
