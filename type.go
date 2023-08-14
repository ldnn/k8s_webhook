package main

import (
	"fmt"
	"net/http"
	"strings"

	v1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/dynamic"
)

var (
	runtimeScheme = runtime.NewScheme()
	codecs        = serializer.NewCodecFactory(runtimeScheme)
	deserializer  = codecs.UniversalDeserializer()

	// (https://github.com/kubernetes/kubernetes/issues/57982)
	defaulter = runtime.ObjectDefaulter(runtimeScheme)
)

const (
	admissionWebhookAnnotationMutateKey = "admission-webhook-ks.cmft/mutate"
	admissionWebhookLabelsKey           = "nci.yunshan.net/vpc"
	admissionWebhookWorkspaceKey        = "kubesphere.io/workspace"
	admissionWebhookAnnotationsKey      = "nci.yunshan.net/ips"
)

type sliceFlag []string

type WebhookServer struct {
	server     *http.Server
	vpcprefix  string
	abnormalws sliceFlag
}

// Webhook Server parameters
type WhSvrParameters struct {
	port           int       // webhook server port
	certFile       string    // path to the x509 certificate for https
	keyFile        string    // path to the x509 private key matching `CertFile`
	sidecarCfgFile string    // path to sidecar injector configuration file
	vpcprefix      string    // vpc label key prefix
	workspaces     sliceFlag // abnormal workspaces
}

type patchOperation struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value,omitempty"`
}

type serverMate struct {
	vpcprefix  string
	abnormalws sliceFlag
	op         v1.Operation
	client     Client
}

type Nets struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`
	Items           []Subnet `json:"items" protobuf:"bytes,2,rep,name=items"`
}

type Subnet struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`
	Spec              SubnetSpec   `json:"spec"`
	Status            SubnetStatus `json:"status"`
}

type SubnetSpec struct {
	CIDR     string `json:"cidr"`
	Gateway  string `json:"gateway"`
	Protocol string `json:"protocol"`
}

type SubnetStatus struct {
	AvailableIPs    string `json:"availableIps"`
	NumAvailableIPs int    `json:"numAvailableIps"`
	NumUsingIPs     int    `json:"numUsingIps"`
	Valid           bool   `json:"valid"`
}

type Vpc struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`
}

type Request struct {
	Operation string `json:"operation"`
}

func (f *sliceFlag) String() string {
	return fmt.Sprintf("%v", []string(*f))
}

func (f *sliceFlag) Set(value string) error {
	split := strings.Split(value, ",")
	*f = split
	return nil
}

type Client struct {
	dynamicClient *dynamic.DynamicClient
}
