package catalogmodel

// Relation types and their inverses for the one typed relation graph
// (orun-service-catalog/data-model.md §3). The forward edge is the one stored;
// the reader (objcatalog) materializes inverses so portals never reverse-walk.
const (
	RelTypeOwnedBy         = "ownedBy"
	RelTypeOwns            = "owns"
	RelTypePartOf          = "partOf"
	RelTypeHasPart         = "hasPart"
	RelTypeDependsOn       = "dependsOn"
	RelTypeDependencyOf    = "dependencyOf"
	RelTypeProvidesAPI     = "providesApi"
	RelTypeAPIProvidedBy   = "apiProvidedBy"
	RelTypeConsumesAPI     = "consumesApi"
	RelTypeAPIConsumedBy   = "apiConsumedBy"
	RelTypeRunsOn          = "runsOn"
	RelTypeHosts           = "hosts"
	RelTypeDeployedTo      = "deployedTo"
	RelTypeHostsDeployment = "hostsDeployment"
	RelTypeComposedBy      = "composedBy"
	RelTypeComposes        = "composes"
)

// relationInverses maps each relation type to its inverse. Bidirectional: the
// reverse mapping is derived in init() so the two stay in lockstep.
var relationInverses = map[string]string{
	RelTypeOwnedBy:     RelTypeOwns,
	RelTypePartOf:      RelTypeHasPart,
	RelTypeDependsOn:   RelTypeDependencyOf,
	RelTypeProvidesAPI: RelTypeAPIProvidedBy,
	RelTypeConsumesAPI: RelTypeAPIConsumedBy,
	RelTypeRunsOn:      RelTypeHosts,
	RelTypeDeployedTo:  RelTypeHostsDeployment,
	RelTypeComposedBy:  RelTypeComposes,
}

func init() {
	// Materialize the reverse direction so InverseRelation is total over every
	// declared edge type without a second hand-maintained table.
	for fwd, inv := range relationInverses {
		if _, ok := relationInverses[inv]; !ok {
			relationInverses[inv] = fwd
		}
	}
}

// InverseRelation returns the inverse of a relation type and whether one is
// known. Unknown types return ("", false).
func InverseRelation(rel string) (string, bool) {
	inv, ok := relationInverses[rel]
	return inv, ok
}

// RelationInclude values carry change-detection plan-selection semantics on an
// edge (data-model.md §3); they must survive resolve → internal/affected.
const (
	RelationIncludeAlways     = "always"
	RelationIncludeIfSelected = "if-selected"
)
