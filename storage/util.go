package esstorage

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/elastic/go-elasticsearch/v8"
	"github.com/elastic/go-elasticsearch/v8/esapi"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/klog/v2"

	internal "github.com/clusterpedia-io/api/clusterpedia"
)

const (
	ElasticSearchLabelFuzzyName = "elasticsearchstorage.clusterpedia.io/fuzzy-name"
)

func applyListOptionToQueryBuilder(builder *QueryBuilder, opts *internal.ListOptions) error {
	if opts.ClusterNames != nil {
		queryItem := NewTerms(ClusterPath, opts.ClusterNames)
		builder.addExpression(queryItem)
	}
	if opts.Namespaces != nil {
		queryItem := NewTerms(NameSpacePath, opts.Namespaces)
		builder.addExpression(queryItem)
	}
	if opts.Names != nil {
		queryItem := NewTerms(NamePath, opts.Names)
		builder.addExpression(queryItem)
	}

	if opts.Since != nil || opts.Before != nil {
		queryItem := &RangeExpression{}
		queryItem = NewRange(CreationTimestampPath, opts.Since, opts.Before)
		builder.addExpression(queryItem)
	}

	if opts.LabelSelector != nil {
		if requirements, selectable := opts.LabelSelector.Requirements(); selectable {
			for _, requirement := range requirements {
				values := requirement.Values().List()
				queryItem := NewTerms(LabelPath, values)
				switch requirement.Operator() {
				case selection.Exists, selection.DoesNotExist, selection.Equals, selection.DoubleEquals:
					builder.addExpression(queryItem)
				case selection.NotEquals, selection.NotIn:
					queryItem.SetLogicType(MustNot)
					builder.addExpression(queryItem)
				}
			}
		}
	}

	if opts.ExtraLabelSelector != nil {
		if requirements, selectable := opts.ExtraLabelSelector.Requirements(); selectable {
			for _, requirement := range requirements {
				switch requirement.Key() {
				case ElasticSearchLabelFuzzyName:
					for _, value := range requirement.Values().List() {
						queryItem := NewFuzzy(NamePath, value)
						switch requirement.Operator() {
						case selection.NotEquals, selection.NotIn:
							queryItem.SetLogicType(MustNot)
						}
						builder.addExpression(queryItem)
					}
				}
			}
		}
	}

	if opts.EnhancedFieldSelector != nil {
		if requirements, selectable := opts.EnhancedFieldSelector.Requirements(); selectable {
			for _, requirement := range requirements {
				var (
					fields      []string
					fieldErrors field.ErrorList
					wsFlag      bool
				)
				for _, f := range requirement.Fields() {
					if f.IsList() {
						fieldErrors = append(fieldErrors, field.Invalid(f.Path(), f.Name(), fmt.Sprintf("Storage<%s>: Not Support list field", StorageName)))
						continue
					}
					fields = append(fields, f.Name())
				}
				if len(fieldErrors) != 0 {
					return apierrors.NewInvalid(schema.GroupKind{Group: internal.GroupName, Kind: "ListOptions"}, "fieldSelector", fieldErrors)
				}
				if fields[0] == "ws" {
					wsFlag = true
				} else {
					fields = append(fields, "")
					copy(fields[1:], fields[0:])
				}
				fields[0] = "object"
				path := strings.Join(fields, ".")
				values := requirement.Values().List()
				switch requirement.Operator() {
				case selection.Exists, selection.DoesNotExist, selection.Equals, selection.DoubleEquals:
					if wsFlag {
						for _, value := range values {
							queryItem := NewWildcard(path, value)
							builder.addExpression(queryItem)
						}
					} else {
						queryItem := NewTerms(path, values)
						builder.addExpression(queryItem)
					}
				case selection.NotEquals, selection.NotIn:
					if wsFlag {
						for _, value := range values {
							queryItem := NewWildcard(path, value)
							queryItem.SetLogicType(MustNot)
							builder.addExpression(queryItem)
						}
					} else {
						queryItem := NewTerms(path, values)
						queryItem.SetLogicType(MustNot)
						builder.addExpression(queryItem)
					}
				}
			}
		}
	}

	size := 500
	if opts.Limit > 0 {
		size = int(opts.Limit)
	}
	offset, _ := strconv.Atoi(opts.Continue)

	var sort []map[string]interface{}
	for _, orderby := range opts.OrderBy {
		queryItem := sortQuery(orderby.Field, orderby.Desc)
		sort = append(sort, queryItem)
	}
	builder.sort = sort
	builder.size = size
	builder.from = offset
	return nil
}

func (s *ResourceStorage) genListQuery(ownerIds []string, opts *internal.ListOptions) (map[string]interface{}, error) {
	builder := NewQueryBuilder()

	err := applyListOptionToQueryBuilder(builder, opts)
	if err != nil {
		return nil, err
	}

	if len(opts.ClusterNames) == 1 && (len(opts.OwnerUID) != 0 || len(opts.OwnerName) != 0) {
		queryItem := NewTerms(OwnerReferencePath, ownerIds)
		builder.addExpression(queryItem)
	}

	groupItem := NewTerms(GroupPath, []string{s.storageVersion.Group})
	builder.addExpression(groupItem)
	versionItem := NewTerms(VersionPath, []string{s.storageVersion.Version})
	builder.addExpression(versionItem)
	resourceItem := NewTerms(ResourcePath, []string{s.storageGroupResource.Resource})
	builder.addExpression(resourceItem)
	return builder.build(), nil
}

func ensureIndex(client *elasticsearch.Client, mapping string, indexName string) error {
	req := esapi.IndicesCreateRequest{
		Index: indexName,
		Body:  strings.NewReader(mapping),
	}
	resp, err := req.Do(context.Background(), client)
	if err != nil {
		klog.Errorf("Error getting response: %v", err)
		return err
	}
	if resp.IsError() {
		msg := resp.String()
		if strings.Contains(resp.String(), "resource_already_exists_exception") {
			klog.Warningf("index %s already exists", indexName)
			return nil
		}
		return fmt.Errorf(msg)
	}
	return nil
}

func simpleMapExtract(path string, object map[string]interface{}) interface{} {
	fields := strings.Split(path, ".")
	var cur interface{}
	cur = object
	for i := range fields {
		field := fields[i]
		mapObj, ok := cur.(map[string]interface{})
		if !ok {
			return nil
		}
		cur = mapObj[field]
	}
	return cur
}

func sortQuery(path string, desc bool) map[string]interface{} {
	sort := map[string]interface{}{}
	switch path {
	case "cluster":
		path = ClusterPath
	case "namespace":
		path = NameSpacePath
	case "name":
		path = NamePath
	case "created_at":
		path = CreationTimestampPath
	case "resource_version":
		path = ResourceVersionPath
	default:
		path = strings.Join([]string{ObjectPath, path}, ".")
	}

	if desc {
		sort[path] = map[string]interface{}{"order": "desc"}
	} else {
		sort[path] = map[string]interface{}{"order": "asc"}
	}
	return sort
}
