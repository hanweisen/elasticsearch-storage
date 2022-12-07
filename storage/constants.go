package esstorage

const (
	ObjectPath            = "object"
	ClusterPath           = "object.metadata.annotations.shadow.clusterpedia.io/cluster-name"
	NameSpacePath         = "object.metadata.namespace"
	NamePath              = "object.metadata.name"
	OwnerReferencePath    = "object.metadata.ownerReferences.uid"
	CreationTimestampPath = "object.metadata.creationTimestamp"
	ResourceVersionPath   = "object.metadata.resourceVersion"
	LabelPath             = "object.metadata.labels"
	GroupPath             = "group"
	VersionPath           = "version"
	ResourcePath          = "resource"
	UIDPath               = "object.metadata.uid"
	ApiVersionPath        = "object.apiVersion"
	KindPath              = "object.kind"
	ObjectMetaPath        = "object.metadata"
)
