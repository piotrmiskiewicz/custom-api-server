package apiserver

import (
    "context"
    "crypto/ecdsa"
    "crypto/elliptic"
    "crypto/rand"
    "crypto/x509"
    "crypto/x509/pkix"
    "encoding/pem"
    "fmt"
    "math/big"
    "net"
    "os"
    "time"

    internal "github.com/piotrmiskiewicz/custom-api-server/pkg/apis/solution"
    "github.com/piotrmiskiewicz/custom-api-server/pkg/apis/solution/v1alpha1"
    registry "github.com/piotrmiskiewicz/custom-api-server/pkg/registry/solution"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/apimachinery/pkg/conversion"
    "k8s.io/apimachinery/pkg/runtime"
    "k8s.io/apimachinery/pkg/runtime/schema"
    "k8s.io/apimachinery/pkg/runtime/serializer"
    apiopenapi "k8s.io/apiserver/pkg/endpoints/openapi"
    "k8s.io/apiserver/pkg/registry/rest"
    genericapiserver "k8s.io/apiserver/pkg/server"
    "k8s.io/apiserver/pkg/server/dynamiccertificates"
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

    // Register field label conversion for Solution so the framework accepts
    // spec.solutionName as a valid field selector in addition to metadata.name/namespace.
    gvk := schema.GroupVersionKind{
        Group:   "solution.piotrmiskiewicz.github.com",
        Version: "v1alpha1",
        Kind:    "Solution",
    }
    Scheme.AddFieldLabelConversionFunc(gvk, func(label, value string) (string, string, error) {
        switch label {
        case "metadata.name", "metadata.namespace", "spec.solutionName":
            return label, value, nil
        default:
            return "", "", fmt.Errorf("%q is not a known field selector: only \"metadata.name\", \"metadata.namespace\", \"spec.solutionName\"", label)
        }
    })
}

func registerConversions(scheme *runtime.Scheme) {
    scheme.AddConversionFunc((*v1alpha1.Solution)(nil), (*internal.Solution)(nil), func(a, b interface{}, scope conversion.Scope) error {
        return v1alpha1.Convert_v1alpha1_Solution_To_solution_Solution(a.(*v1alpha1.Solution), b.(*internal.Solution), scope)
    })
    scheme.AddConversionFunc((*internal.Solution)(nil), (*v1alpha1.Solution)(nil), func(a, b interface{}, scope conversion.Scope) error {
        return v1alpha1.Convert_solution_Solution_To_v1alpha1_Solution(a.(*internal.Solution), b.(*v1alpha1.Solution), scope)
    })
    scheme.AddConversionFunc((*v1alpha1.SolutionList)(nil), (*internal.SolutionList)(nil), func(a, b interface{}, scope conversion.Scope) error {
        return v1alpha1.Convert_v1alpha1_SolutionList_To_solution_SolutionList(a.(*v1alpha1.SolutionList), b.(*internal.SolutionList), scope)
    })
    scheme.AddConversionFunc((*internal.SolutionList)(nil), (*v1alpha1.SolutionList)(nil), func(a, b interface{}, scope conversion.Scope) error {
        return v1alpha1.Convert_solution_SolutionList_To_v1alpha1_SolutionList(a.(*internal.SolutionList), b.(*v1alpha1.SolutionList), scope)
    })
}

// New builds and returns a configured GenericAPIServer.
// certFile and keyFile are paths to the TLS certificate and key.
// If both are empty, a self-signed certificate is generated in a temp directory.
// addr is the listen address (e.g. ":8443"); if empty defaults to ":8443".
func New(certFile, keyFile, addr string) (*genericapiserver.GenericAPIServer, error) {
    if addr == "" {
        addr = ":8443"
    }
    recommendedConfig := genericapiserver.NewRecommendedConfig(Codecs)

    // EffectiveVersion is required by Complete().
    recommendedConfig.EffectiveVersion = apiservercompat.DefaultBuildEffectiveVersion()

    // Configure TLS. If no cert/key paths are provided, generate a self-signed cert.
    if err := configureTLS(recommendedConfig, certFile, keyFile, addr); err != nil {
        return nil, fmt.Errorf("TLS setup: %w", err)
    }

    // LoopbackClientConfig is required by Complete().New().
    recommendedConfig.LoopbackClientConfig = &restclient.Config{
        Host:            "https://localhost:8443",
        TLSClientConfig: restclient.TLSClientConfig{Insecure: true},
    }

    // Disable post-start hooks that require etcd or cluster connectivity.
    recommendedConfig.DisabledPostStartHooks.Insert(
        "max-in-flight-filter",
        "storage-object-count-tracker-hook",
        "priority-and-fairness-filter",
    )

    // OpenAPI definition keys must match GetCanonicalTypeName, which uses OpenAPIModelName()
    // if defined (all k8s.io/apimachinery v0.36+ types have it), else the Go import path.
    // We include all metav1 types to cover every type the framework routes may reference,
    // plus our custom types (no OpenAPIModelName(), so keyed by Go import path).
    getDefinitions := func(ref openapicommon.ReferenceCallback) map[string]openapicommon.OpenAPIDefinition {
        return map[string]openapicommon.OpenAPIDefinition{
            // k8s.io/apimachinery/pkg/version
            "io.k8s.apimachinery.pkg.version.Info": {Schema: specv3.Schema{}},
            // k8s.io/apimachinery/pkg/apis/meta/v1 — all types
            "io.k8s.apimachinery.pkg.apis.meta.v1.APIGroup":                  {Schema: specv3.Schema{}},
            "io.k8s.apimachinery.pkg.apis.meta.v1.APIGroupList":              {Schema: specv3.Schema{}},
            "io.k8s.apimachinery.pkg.apis.meta.v1.APIResource":               {Schema: specv3.Schema{}},
            "io.k8s.apimachinery.pkg.apis.meta.v1.APIResourceList":           {Schema: specv3.Schema{}},
            "io.k8s.apimachinery.pkg.apis.meta.v1.APIVersions":               {Schema: specv3.Schema{}},
            "io.k8s.apimachinery.pkg.apis.meta.v1.ApplyOptions":              {Schema: specv3.Schema{}},
            "io.k8s.apimachinery.pkg.apis.meta.v1.Condition":                 {Schema: specv3.Schema{}},
            "io.k8s.apimachinery.pkg.apis.meta.v1.CreateOptions":             {Schema: specv3.Schema{}},
            "io.k8s.apimachinery.pkg.apis.meta.v1.DeleteOptions":             {Schema: specv3.Schema{}},
            "io.k8s.apimachinery.pkg.apis.meta.v1.Duration":                  {Schema: specv3.Schema{}},
            "io.k8s.apimachinery.pkg.apis.meta.v1.FieldSelectorRequirement":  {Schema: specv3.Schema{}},
            "io.k8s.apimachinery.pkg.apis.meta.v1.FieldsV1":                  {Schema: specv3.Schema{}},
            "io.k8s.apimachinery.pkg.apis.meta.v1.GetOptions":                {Schema: specv3.Schema{}},
            "io.k8s.apimachinery.pkg.apis.meta.v1.GroupKind":                 {Schema: specv3.Schema{}},
            "io.k8s.apimachinery.pkg.apis.meta.v1.GroupResource":             {Schema: specv3.Schema{}},
            "io.k8s.apimachinery.pkg.apis.meta.v1.GroupVersion":              {Schema: specv3.Schema{}},
            "io.k8s.apimachinery.pkg.apis.meta.v1.GroupVersionForDiscovery":  {Schema: specv3.Schema{}},
            "io.k8s.apimachinery.pkg.apis.meta.v1.GroupVersionKind":          {Schema: specv3.Schema{}},
            "io.k8s.apimachinery.pkg.apis.meta.v1.GroupVersionResource":      {Schema: specv3.Schema{}},
            "io.k8s.apimachinery.pkg.apis.meta.v1.InternalEvent":             {Schema: specv3.Schema{}},
            "io.k8s.apimachinery.pkg.apis.meta.v1.LabelSelector":             {Schema: specv3.Schema{}},
            "io.k8s.apimachinery.pkg.apis.meta.v1.LabelSelectorRequirement":  {Schema: specv3.Schema{}},
            "io.k8s.apimachinery.pkg.apis.meta.v1.List":                      {Schema: specv3.Schema{}},
            "io.k8s.apimachinery.pkg.apis.meta.v1.ListMeta":                  {Schema: specv3.Schema{}},
            "io.k8s.apimachinery.pkg.apis.meta.v1.ListOptions":               {Schema: specv3.Schema{}},
            "io.k8s.apimachinery.pkg.apis.meta.v1.ManagedFieldsEntry":        {Schema: specv3.Schema{}},
            "io.k8s.apimachinery.pkg.apis.meta.v1.MicroTime":                 {Schema: specv3.Schema{}},
            "io.k8s.apimachinery.pkg.apis.meta.v1.ObjectMeta":                {Schema: specv3.Schema{}},
            "io.k8s.apimachinery.pkg.apis.meta.v1.OwnerReference":            {Schema: specv3.Schema{}},
            "io.k8s.apimachinery.pkg.apis.meta.v1.PartialObjectMetadata":     {Schema: specv3.Schema{}},
            "io.k8s.apimachinery.pkg.apis.meta.v1.PartialObjectMetadataList": {Schema: specv3.Schema{}},
            "io.k8s.apimachinery.pkg.apis.meta.v1.Patch":                     {Schema: specv3.Schema{}},
            "io.k8s.apimachinery.pkg.apis.meta.v1.PatchOptions":              {Schema: specv3.Schema{}},
            "io.k8s.apimachinery.pkg.apis.meta.v1.Preconditions":             {Schema: specv3.Schema{}},
            "io.k8s.apimachinery.pkg.apis.meta.v1.RootPaths":                 {Schema: specv3.Schema{}},
            "io.k8s.apimachinery.pkg.apis.meta.v1.ServerAddressByClientCIDR": {Schema: specv3.Schema{}},
            "io.k8s.apimachinery.pkg.apis.meta.v1.Status":                    {Schema: specv3.Schema{}},
            "io.k8s.apimachinery.pkg.apis.meta.v1.StatusCause":               {Schema: specv3.Schema{}},
            "io.k8s.apimachinery.pkg.apis.meta.v1.StatusDetails":             {Schema: specv3.Schema{}},
            "io.k8s.apimachinery.pkg.apis.meta.v1.Table":                     {Schema: specv3.Schema{}},
            "io.k8s.apimachinery.pkg.apis.meta.v1.TableColumnDefinition":     {Schema: specv3.Schema{}},
            "io.k8s.apimachinery.pkg.apis.meta.v1.TableOptions":              {Schema: specv3.Schema{}},
            "io.k8s.apimachinery.pkg.apis.meta.v1.TableRow":                  {Schema: specv3.Schema{}},
            "io.k8s.apimachinery.pkg.apis.meta.v1.TableRowCondition":         {Schema: specv3.Schema{}},
            "io.k8s.apimachinery.pkg.apis.meta.v1.Time":                      {Schema: specv3.Schema{}},
            "io.k8s.apimachinery.pkg.apis.meta.v1.Timestamp":                 {Schema: specv3.Schema{}},
            "io.k8s.apimachinery.pkg.apis.meta.v1.TypeMeta":                  {Schema: specv3.Schema{}},
            "io.k8s.apimachinery.pkg.apis.meta.v1.UpdateOptions":             {Schema: specv3.Schema{}},
            "io.k8s.apimachinery.pkg.apis.meta.v1.WatchEvent":                {Schema: specv3.Schema{}},
            // Custom types — no OpenAPIModelName(), keyed by Go import path.
            // Full structural schemas are required so the field manager (server-side apply)
            // can build a typed representation; empty schemas cause "[SHOULD NOT HAPPEN]" log spam.
            "github.com/piotrmiskiewicz/custom-api-server/pkg/apis/solution/v1alpha1.SolutionSpec": {
                Schema: specv3.Schema{
                    SchemaProps: specv3.SchemaProps{
                        Type: specv3.StringOrArray{"object"},
                        Properties: map[string]specv3.Schema{
                            "solutionName": {SchemaProps: specv3.SchemaProps{Type: specv3.StringOrArray{"string"}}},
                        },
                        Required: []string{"solutionName"},
                    },
                },
            },
            "github.com/piotrmiskiewicz/custom-api-server/pkg/apis/solution/v1alpha1.SolutionStatus": {
                Schema: specv3.Schema{
                    SchemaProps: specv3.SchemaProps{
                        Type: specv3.StringOrArray{"object"},
                        Properties: map[string]specv3.Schema{
                            "phase": {SchemaProps: specv3.SchemaProps{
                                Type: specv3.StringOrArray{"string"},
                                Enum: []interface{}{"Pending", "Scheduling", "Deploying", "Running", "Failed", "Deleting", ""},
                            }},
                            "conditions": {SchemaProps: specv3.SchemaProps{
                                Type: specv3.StringOrArray{"array"},
                                Items: &specv3.SchemaOrArray{Schema: &specv3.Schema{
                                    SchemaProps: specv3.SchemaProps{Ref: specv3.MustCreateRef("#/definitions/io.k8s.apimachinery.pkg.apis.meta.v1.Condition")},
                                }},
                            }},
                        },
                    },
                },
            },
            "github.com/piotrmiskiewicz/custom-api-server/pkg/apis/solution/v1alpha1.Solution": {
                Schema: specv3.Schema{
                    SchemaProps: specv3.SchemaProps{
                        Type: specv3.StringOrArray{"object"},
                        Properties: map[string]specv3.Schema{
                            "apiVersion": {SchemaProps: specv3.SchemaProps{Type: specv3.StringOrArray{"string"}}},
                            "kind":       {SchemaProps: specv3.SchemaProps{Type: specv3.StringOrArray{"string"}}},
                            "metadata":   {SchemaProps: specv3.SchemaProps{Ref: specv3.MustCreateRef("#/definitions/io.k8s.apimachinery.pkg.apis.meta.v1.ObjectMeta")}},
                            "spec":       {SchemaProps: specv3.SchemaProps{Ref: specv3.MustCreateRef("#/definitions/github.com/piotrmiskiewicz/custom-api-server/pkg/apis/solution/v1alpha1.SolutionSpec")}},
                            "status":     {SchemaProps: specv3.SchemaProps{Ref: specv3.MustCreateRef("#/definitions/github.com/piotrmiskiewicz/custom-api-server/pkg/apis/solution/v1alpha1.SolutionStatus")}},
                        },
                    },
                },
            },
            "github.com/piotrmiskiewicz/custom-api-server/pkg/apis/solution/v1alpha1.SolutionList": {
                Schema: specv3.Schema{
                    SchemaProps: specv3.SchemaProps{
                        Type: specv3.StringOrArray{"object"},
                        Properties: map[string]specv3.Schema{
                            "apiVersion": {SchemaProps: specv3.SchemaProps{Type: specv3.StringOrArray{"string"}}},
                            "kind":       {SchemaProps: specv3.SchemaProps{Type: specv3.StringOrArray{"string"}}},
                            "metadata":   {SchemaProps: specv3.SchemaProps{Ref: specv3.MustCreateRef("#/definitions/io.k8s.apimachinery.pkg.apis.meta.v1.ListMeta")}},
                            "items": {SchemaProps: specv3.SchemaProps{
                                Type: specv3.StringOrArray{"array"},
                                Items: &specv3.SchemaOrArray{Schema: &specv3.Schema{
                                    SchemaProps: specv3.SchemaProps{Ref: specv3.MustCreateRef("#/definitions/github.com/piotrmiskiewicz/custom-api-server/pkg/apis/solution/v1alpha1.Solution")},
                                }},
                            }},
                        },
                        Required: []string{"items"},
                    },
                },
            },
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

    store, statusREST, err := newStorage()
    if err != nil {
        return nil, err
    }

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

// newStorage creates the appropriate storage backend based on the STORAGE_BACKEND
// environment variable. Supported values: "postgres" (requires POSTGRES_DSN),
// anything else (including empty) uses the in-memory backend.
func newStorage() (registry.Storage, *registry.StatusREST, error) {
    switch os.Getenv("STORAGE_BACKEND") {
    case "postgres":
        dsn := os.Getenv("POSTGRES_DSN")
        if dsn == "" {
            return nil, nil, fmt.Errorf("POSTGRES_DSN must be set when STORAGE_BACKEND=postgres")
        }
        pgStore, err := registry.NewPostgresStorage(context.Background(), dsn)
        if err != nil {
            return nil, nil, fmt.Errorf("postgres storage: %w", err)
        }
        return pgStore, registry.NewStatusREST(pgStore), nil
    default:
        memStore := registry.NewSolutionStorage()
        return memStore, registry.NewStatusREST(memStore), nil
    }
}

// configureTLS sets up SecureServingInfo on the config.
// If certFile and keyFile are provided, they are used directly.
// Otherwise a self-signed certificate is generated in a temporary directory.
func configureTLS(cfg *genericapiserver.RecommendedConfig, certFile, keyFile, addr string) error {
    if certFile == "" || keyFile == "" {
        var err error
        certFile, keyFile, err = generateSelfSignedCert()
        if err != nil {
            return fmt.Errorf("generate self-signed cert: %w", err)
        }
    }

    certProvider, err := dynamiccertificates.NewDynamicServingContentFromFiles("serving", certFile, keyFile)
    if err != nil {
        return fmt.Errorf("load cert: %w", err)
    }

    ln, err := net.Listen("tcp", addr)
    if err != nil {
        return fmt.Errorf("listen :8443: %w", err)
    }

    cfg.SecureServing = &genericapiserver.SecureServingInfo{
        Listener: ln,
        Cert:     certProvider,
    }
    return nil
}

// generateSelfSignedCert writes a self-signed ECDSA cert+key to a temp directory
// and returns their paths. The cert is valid for 10 years and covers localhost and
// the in-cluster DNS name for the service.
func generateSelfSignedCert() (certFile, keyFile string, err error) {
    key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
    if err != nil {
        return "", "", err
    }

    tmpl := &x509.Certificate{
        SerialNumber: big.NewInt(1),
        Subject:      pkix.Name{CommonName: "custom-api-server"},
        NotBefore:    time.Now(),
        NotAfter:     time.Now().Add(10 * 365 * 24 * time.Hour),
        KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
        ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
        IsCA:         true,
        DNSNames: []string{
            "localhost",
            "custom-api-server",
            "custom-api-server.default",
            "custom-api-server.default.svc",
            "custom-api-server.default.svc.cluster.local",
        },
        IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
    }

    certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
    if err != nil {
        return "", "", err
    }

    dir, err := os.MkdirTemp("", "custom-api-server-tls-*")
    if err != nil {
        return "", "", err
    }

    certFile = dir + "/tls.crt"
    keyFile = dir + "/tls.key"

    cf, err := os.Create(certFile)
    if err != nil {
        return "", "", err
    }
    defer cf.Close()
    if err := pem.Encode(cf, &pem.Block{Type: "CERTIFICATE", Bytes: certDER}); err != nil {
        return "", "", err
    }

    kf, err := os.Create(keyFile)
    if err != nil {
        return "", "", err
    }
    defer kf.Close()
    keyDER, err := x509.MarshalECPrivateKey(key)
    if err != nil {
        return "", "", err
    }
    if err := pem.Encode(kf, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}); err != nil {
        return "", "", err
    }

    return certFile, keyFile, nil
}
