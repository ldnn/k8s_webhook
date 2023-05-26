package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/golang/glog"
	v1 "k8s.io/api/admission/v1"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
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
)

type WebhookServer struct {
	server    *http.Server
	vpcprefix string
}

// Webhook Server parameters
type WhSvrParameters struct {
	port           int    // webhook server port
	certFile       string // path to the x509 certificate for https
	keyFile        string // path to the x509 private key matching `CertFile`
	sidecarCfgFile string // path to sidecar injector configuration file
	vpcprefix      string // vpc label key prefix
}

type patchOperation struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value,omitempty"`
}

func init() {
	_ = corev1.AddToScheme(runtimeScheme)
	_ = admissionregistrationv1.AddToScheme(runtimeScheme)
	// defaulting with webhooks:
	// https://github.com/kubernetes/kubernetes/issues/57982
	_ = v1.AddToScheme(runtimeScheme)
}

func admissionRequired(admissionWebhookAnnotationMutateKey string, metadata *metav1.ObjectMeta) bool {
	annotations := metadata.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}

	var required bool

	if _, ok := annotations[admissionWebhookAnnotationMutateKey]; ok {
		switch strings.ToLower(annotations[admissionWebhookAnnotationMutateKey]) {
		default:
			required = true
		case "n", "no", "false", "off", "disable", "":
			required = false
		}
	} else {
		required = true
	}

	return required
}

func mutationRequired(metadata *metav1.ObjectMeta) bool {
	required := admissionRequired(admissionWebhookAnnotationMutateKey, metadata)

	labels := metadata.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}

	if _, ok := labels[admissionWebhookLabelsKey]; ok {
		required = false
	}

	glog.Infof("Mutation policy for %v: required:%v", metadata.Name, required)
	return required
}

func updateLabels(target map[string]string, added map[string]string) (patch []patchOperation) {
	values := make(map[string]string)
	if len(target) > 0 {
		values = target
	}

	for key, value := range added {
		values[key] = value
	}
	patch = append(patch, patchOperation{
		Op:    "add",
		Path:  "/metadata/labels",
		Value: values,
	})
	return patch
}

func createPatch(availableLabels map[string]string, labels map[string]string) ([]byte, error) {
	var patch []patchOperation
	patch = append(patch, updateLabels(availableLabels, labels)...)

	return json.Marshal(patch)
}

// main mutation process
func (whsvr *WebhookServer) mutate(ar *v1.AdmissionReview) *v1.AdmissionResponse {
	req := ar.Request
	var (
		objectMeta   *metav1.ObjectMeta
		resourceName string
	)

	glog.Infof("AdmissionReview for Kind=%v, Name=%v (%v) UID=%v patchOperation=%v UserInfo=%v",
		req.Kind, req.Name, req.UID, req.Operation, req.UserInfo)

	switch req.Kind.Kind {
	case "Namespace":
		var namespace corev1.Namespace
		if err := json.Unmarshal(req.Object.Raw, &namespace); err != nil {
			glog.Errorf("Could not unmarshal raw object: %v", err)
			return &v1.AdmissionResponse{
				Result: &metav1.Status{
					Message: err.Error(),
				},
			}
		}
		resourceName, objectMeta = namespace.Name, &namespace.ObjectMeta
	default:
		msg := fmt.Sprintf("\nNot support for this Kind of resource  %v", req.Kind.Kind)
		glog.Infof(msg)
		return &v1.AdmissionResponse{
			Result: &metav1.Status{
				Message: msg,
			},
		}
	}

	if !mutationRequired(objectMeta) {
		glog.Infof("Skipping validation for %s due to policy check", resourceName)
		return &v1.AdmissionResponse{
			Allowed: true,
		}
	}

	addLabels := make(map[string]string)

	var workspace string

	if _, ok := objectMeta.Labels[admissionWebhookWorkspaceKey]; !ok {
		msg := "Invalid namespace: not in workspace"
		glog.Errorf(msg)
		return &v1.AdmissionResponse{
			Result: &metav1.Status{
				Message: msg,
			},
		}
	} else {
		workspace = objectMeta.Labels[admissionWebhookWorkspaceKey]
	}

	switch workspace {
	case "system-workspace":
		addLabels[admissionWebhookLabelsKey] = "default"
	default:
		addLabels[admissionWebhookLabelsKey] = whsvr.vpcprefix + workspace
	}

	patchBytes, err := createPatch(objectMeta.Labels, addLabels)
	if err != nil {
		return &v1.AdmissionResponse{
			Result: &metav1.Status{
				Message: err.Error(),
			},
		}
	}

	glog.Infof("AdmissionResponse: patch=%v\n", string(patchBytes))
	return &v1.AdmissionResponse{
		Allowed: true,
		Patch:   patchBytes,
		PatchType: func() *v1.PatchType {
			pt := v1.PatchTypeJSONPatch
			return &pt
		}(),
	}
}

// Serve method for webhook server
func (whsvr *WebhookServer) serve(w http.ResponseWriter, r *http.Request) {
	var body []byte
	if r.Body != nil {
		if data, err := ioutil.ReadAll(r.Body); err == nil {
			body = data
		}
	}
	if len(body) == 0 {
		glog.Error("empty body")
		http.Error(w, "empty body", http.StatusBadRequest)
		return
	}

	var admission v1.AdmissionReview
	codefc := serializer.NewCodecFactory(runtime.NewScheme())
	decoder := codefc.UniversalDeserializer()
	_, _, err := decoder.Decode(body, nil, &admission)

	if err != nil {
		msg := fmt.Sprintf("Request could not be decoded: %v", err)
		glog.Error(msg)
		http.Error(w, msg, http.StatusBadRequest)
		return
	}

	if admission.Request == nil {
		glog.Error(fmt.Sprintf("admission review can't be used: Request field is nil"))
		http.Error(w, fmt.Errorf("admission review can't be used: Request field is nil").Error(), http.StatusBadRequest)
		return
	}

	// verify the content type is accurate
	contentType := r.Header.Get("Content-Type")
	if contentType != "application/json" {
		glog.Errorf("Content-Type=%s, expect application/json", contentType)
		http.Error(w, "invalid Content-Type, expect `application/json`", http.StatusUnsupportedMediaType)
		return
	}

	var admissionResponse *v1.AdmissionResponse
	ar := v1.AdmissionReview{}
	if _, _, err := deserializer.Decode(body, nil, &ar); err != nil {
		fmt.Println("heh")
		glog.Errorf("Can't decode body: %v", err)
		admissionResponse = &v1.AdmissionResponse{
			Result: &metav1.Status{
				Message: err.Error(),
			},
		}
	} else {
		fmt.Println(r.URL.Path)
		if r.URL.Path == "/mutate" {
			admissionResponse = whsvr.mutate(&ar)
		}
	}

	admissionReview := v1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			Kind:       admission.Kind,
			APIVersion: admission.APIVersion,
		},
	}
	if admissionResponse != nil {
		admissionReview.Response = admissionResponse
		if ar.Request != nil {
			admissionReview.Response.UID = ar.Request.UID
		}
	}

	resp, err := json.Marshal(admissionReview)
	if err != nil {
		glog.Errorf("Can't encode response: %v", err)
		http.Error(w, fmt.Sprintf("could not encode response: %v", err), http.StatusInternalServerError)
	}
	glog.Infof("Ready to write reponse ...")
	if _, err := w.Write(resp); err != nil {
		glog.Errorf("Can't write response: %v", err)
		http.Error(w, fmt.Sprintf("could not write response: %v", err), http.StatusInternalServerError)
	}
}
