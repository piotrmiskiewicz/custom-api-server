package apiserver

import (
	internal "github.com/piotrmiskiewicz/custom-api-server/pkg/apis/solution"
	"github.com/piotrmiskiewicz/custom-api-server/pkg/apis/solution/v1alpha1"
	registry "github.com/piotrmiskiewicz/custom-api-server/pkg/registry/solution"
	"k8s.io/apimachinery/pkg/conversion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	apiopenapi "k8s.io/apiserver/pkg/endpoints/openapi"
	"k8s.io/apiserver/pkg/registry/rest"
	genericapiserver "k8s.io/apiserver/pkg/server"
	apiservercompat "k8s.io/apiserver/pkg/util/compatibility"
	restclient "k8s.io/client-go/rest"
	openapicommon "k8s.io/kube-openapi/pkg/common"
	specv3 "k8s.io/kube-openapi/pkg/validation/spec"
)

var (
	// Scheme holds all registered types.
	Scheme = runtime.NewScheme()
	// Codecs is the CodecFactory built from Scheme.
	Codecs = serializer.NewCodecFactory(Scheme)
)

func init() {
	internal.AddToScheme(Scheme)
	v1alpha1.AddToScheme(Scheme)
	registerConversions(Scheme)
	metav1.AddToGroupVersion(Scheme, schema.GroupVersion{Version: "v1"})
	unversioned := schema.GroupVersion{Group: "", Version: "v1"}
	Scheme.AddUnversionedTypes(unversioned,
		&metav1.Status{},
		&metav1.APIVersions{},
		&metav1.APIGroupList{},
		&metav1.APIGroup{},
		&metav1.APIResourceList{},
	)
}

func registerConversions(scheme *runtime.Scheme) {
	scheme.AddConversionFunc((*v1alpha1.Solution)(nil), (*internal.Solution)(nil), func(a, b interface{}, scope conversion.Scope) error {
		return v1alpha1.Convert_v1alpha1_Solution_To_solution_Solution(a.(*v1alpha1.Solution), b.(*internal.Solution), scope)
	})
	scheme.AddConversionFunc((*internal.Solution)(nil), (*v1alpha1.Solution)(nil), func(a, b interface{}, scope conversion.Scope) error {
		return v1alpha1.Convert_solution_Solution_To_v1alpha1_Solution(a.(*internal.Solution), b.(*v1alpha1.Solution), scope)
	})
}

// New builds and returns a configured GenericAPIServer.
func New() (*genericapiserver.GenericAPIServer, error) {
	recommendedConfig := genericapiserver.NewRecommendedConfig(Codecs)

	// Disable TLS — run plain HTTP only.
	recommendedConfig.SecureServing = nil

	// ExternalAddress is required when SecureServing is nil.
	recommendedConfig.ExternalAddress = "localhost:8080"

	// EffectiveVersion is required by Complete().
	recommendedConfig.EffectiveVersion = apiservercompat.DefaultBuildEffectiveVersion()

	// LoopbackClientConfig is required by Complete().New(); point it at the
	// insecure address we'll use in main.go.
	recommendedConfig.LoopbackClientConfig = &restclient.Config{
		Host: "http://localhost:8080",
	}

	// Stub OpenAPI so the framework doesn't panic.
	// We provide a minimal GetDefinitions that returns empty schemas for all
	// versioned types, satisfying the SSA type converter without code generation.
	getDefinitions := func(ref openapicommon.ReferenceCallback) map[string]openapicommon.OpenAPIDefinition {
		return map[string]openapicommon.OpenAPIDefinition{
			"github.com/piotrmiskiewicz/custom-api-server/pkg/apis/solution/v1alpha1.Solution":     {Schema: specv3.Schema{}},
			"github.com/piotrmiskiewicz/custom-api-server/pkg/apis/solution/v1alpha1.SolutionList": {Schema: specv3.Schema{}},
			"github.com/piotrmiskiewicz/custom-api-server/pkg/apis/solution/v1alpha1.SolutionSpec": {Schema: specv3.Schema{}},
			"k8s.io/apimachinery/pkg/apis/meta/v1.ObjectMeta":                                      {Schema: specv3.Schema{}},
			"k8s.io/apimachinery/pkg/apis/meta/v1.ListMeta":                                        {Schema: specv3.Schema{}},
		}
	}
	recommendedConfig.Config.OpenAPIConfig = genericapiserver.DefaultOpenAPIConfig(
		getDefinitions,
		apiopenapi.NewDefinitionNamer(Scheme),
	)
	recommendedConfig.Config.OpenAPIConfig.Info.Title = "custom-api-server"
	recommendedConfig.Config.OpenAPIConfig.Info.Version = "0.1.0"
	recommendedConfig.Config.OpenAPIV3Config = genericapiserver.DefaultOpenAPIV3Config(
		getDefinitions,
		apiopenapi.NewDefinitionNamer(Scheme),
	)
	recommendedConfig.Config.OpenAPIV3Config.Info.Title = "custom-api-server"
	recommendedConfig.Config.OpenAPIV3Config.Info.Version = "0.1.0"

	genericServer, err := recommendedConfig.Complete().New("custom-api-server", genericapiserver.NewEmptyDelegate())
	if err != nil {
		return nil, err
	}

	store := registry.NewSolutionStorage()
	statusREST := registry.NewStatusREST(store)

	apiGroupInfo := genericapiserver.NewDefaultAPIGroupInfo(
		"solution.piotrmiskiewicz.github.com",
		Scheme,
		metav1.ParameterCodec,
		Codecs,
	)
	apiGroupInfo.VersionedResourcesStorageMap["v1alpha1"] = map[string]rest.Storage{
		"solutions":        store,
		"solutions/status": statusREST,
	}

	if err := genericServer.InstallAPIGroup(&apiGroupInfo); err != nil {
		return nil, err
	}
	return genericServer, nil
}
