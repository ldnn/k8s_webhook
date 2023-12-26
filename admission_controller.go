package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/golang/glog"
	v1 "k8s.io/api/admission/v1"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"
	"kubesphere.io/api/tenant/v1alpha1"
)

func mutateDeploy(svmate serverMate, deploy *appsv1.Deployment) *v1.AdmissionResponse {
	var (
		objectMeta, specMeta            *metav1.ObjectMeta
		resourceName, resourceNamespace string
	)
	resourceName, resourceNamespace, objectMeta, specMeta = deploy.Name, deploy.Namespace, &deploy.ObjectMeta, &deploy.Spec.Template.ObjectMeta

	var patches []patchOperation

	//判断是否需要修改
	if !admissionRequired(admissionWebhookAnnotationMutateKey, objectMeta) {
		glog.Infof("Skipping validation for %s due to policy check", resourceName)
		return &v1.AdmissionResponse{
			Allowed: true,
		}
	}

	client := svmate.client

	//通过标签判断是否为网关deployment
	if !checkKsIngress(objectMeta, resourceName) {
		patchBytes, err := json.Marshal(patches)
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

	//获取所在子网的前15个ip地址，生成annotation键值对
	subnet := client.getSubnet(resourceNamespace)
	if subnet == "ERROR" {
		msg := fmt.Sprintf("namespace: \"%v\" 没有关联到子网，请排查sdn网络", resourceNamespace)
		glog.Errorf(msg)
		return &v1.AdmissionResponse{
			Result: &metav1.Status{
				Message: msg,
			},
		}
	}
	ips := createAnnotation(subnet)

	//ips := make(map[string]string)
	//ips["nci.yunshan.net/ips"] = "10.64.88.1,10.64.88.2,10.64.88.3,10.64.88.4,10.64.88.5,10.64.88.6,10.64.88.7,10.64.88.8,10.64.88.9,10.64.88.10,10.64.88.11,10.64.88.12,10.64.88.13,10.64.88.14,10.64.88.15"

	if !checkAnnotation(specMeta, ips) {
		glog.Infof("%s已经注入了固定ip地址,", resourceName)

		patchBytes, err := json.Marshal(patches)
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

	patch := createPatch(specMeta.Annotations, ips)

	for i := range patch {
		patches = append(patches, patch[i])
	}

	patchBytes, err := json.Marshal(patches)
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

func mutateNamespce(svmate serverMate, namespace *corev1.Namespace) *v1.AdmissionResponse {
	var (
		objectMeta   *metav1.ObjectMeta
		resourceName string
	)

	resourceName, objectMeta = namespace.Name, &namespace.ObjectMeta
	client := svmate.client

	//判断是否进行修改
	if !admissionRequired(admissionWebhookAnnotationMutateKey, objectMeta) {
		glog.Infof("Skipping validation for %s due to policy check", resourceName)
		return &v1.AdmissionResponse{
			Allowed: true,
		}
	}

	addLabels := make(map[string]string)

	var workspace string

	//判断有没有workspace标签
	if _, ok := objectMeta.Labels[admissionWebhookWorkspaceKey]; !ok {
		msg := fmt.Sprintf("Invalid namespace: \"%v\" not in workspace", objectMeta.Name)
		glog.Errorf(msg)
		return &v1.AdmissionResponse{
			Result: &metav1.Status{
				Message: msg,
			},
		}
	} else {
		workspace = objectMeta.Labels[admissionWebhookWorkspaceKey]
	}

	//判断workspace是否存在
	if !client.workspaceExist(workspace) {
		msg := fmt.Sprintf("业务空间: \"%v\" 不存在", workspace)
		glog.Errorf(msg)
		return &v1.AdmissionResponse{
			Result: &metav1.Status{
				Message: msg,
			},
		}
	}
	//生成vpc名
	if svmate.vpcprefix == "default" {
		addLabels[admissionWebhookLabelsKey] = "default"
	} else {
		addLabels[admissionWebhookLabelsKey] = generateVpcName(workspace, svmate)
	}

	if !checkLabel(objectMeta, addLabels[admissionWebhookLabelsKey]) {
		glog.Infof("Skipping validation for %s due to policy check", resourceName)
		return &v1.AdmissionResponse{
			Allowed: true,
		}
	}

	pathes := createPatch(objectMeta.Labels, addLabels)

	patchBytes, err := json.Marshal(pathes)
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

	//判断是否需要进行修改
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
	glog.Infof("Mutation policy for %v: required:%v", metadata.Name, required)
	return required
}

func checkKsIngress(metaData *metav1.ObjectMeta, resourceName string) bool {
	if _, ok := metaData.Labels["app.kubernetes.io/component"]; !ok {
		glog.Infof("%s不是平台网关应用，跳过注入固定ip地址,", resourceName)
		return false
	}

	if _, ok := metaData.Labels["app.kubernetes.io/name"]; !ok {
		glog.Infof("%s不是平台网关应用，跳过注入固定ip地址,", resourceName)
		return false
	}

	if metaData.Labels["app.kubernetes.io/component"] != "controller" || metaData.Labels["app.kubernetes.io/name"] != "ingress-nginx" {
		glog.Infof("%s不是平台网关应用，跳过注入固定ip地址,", resourceName)
		return false
	}

	return true
}

func checkLabel(metaData *metav1.ObjectMeta, targetLabel string) bool {

	required := true
	labels := metaData.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}

	//检查标签是否已经存在
	if _, ok := labels[admissionWebhookLabelsKey]; ok {
		if labels[admissionWebhookLabelsKey] == targetLabel {
			required = false
		}

	}

	glog.Infof("The label %v exists in this namespace: %v", targetLabel, metaData.Name)
	return required
}

func generateVpcName(workspace string, svmate serverMate) string {
	ws := make(map[string]bool)
	var vpcName string

	for _, v := range svmate.abnormalws {
		ws[v] = true
	}

	//处理历史中的不规范vpc命名
	if ws[workspace] {
		switch workspace {
		case "midcloud":
			vpcName = "db-middleware"
		case "bigdata-usercenter2":
			vpcName = "bigdata-jh-ks"
		default:
			vpcName = workspace
		}
	} else {
		switch workspace {
		case "system-workspace":
			vpcName = "default"
		case "firefly":
			vpcName = "default"
		default:
			labelValue := []string{svmate.vpcprefix, workspace}
			vpcName = strings.Join(labelValue, "-")
		}
	}

	return vpcName
}

func checkAnnotation(meta *metav1.ObjectMeta, targetAnnotation map[string]string) bool {

	required := true
	annotations := meta.Annotations
	if annotations == nil {
		annotations = map[string]string{}
	}

	//检查描述是否存在
	if _, ok := annotations[admissionWebhookAnnotationsKey]; ok {
		if annotations[admissionWebhookAnnotationsKey] == targetAnnotation[admissionWebhookAnnotationsKey] {
			required = false
		}

	}

	glog.Infof("The annotation %v exists in this Ingress", targetAnnotation)
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

func updateAnnotations(target map[string]string, added map[string]string) (patch []patchOperation) {
	values := make(map[string]string)
	if len(target) > 0 {
		values = target
	}

	for key, value := range added {
		values[key] = value
	}

	patch = append(patch, patchOperation{
		Op:    "add",
		Path:  "/spec/template/metadata/annotations",
		Value: values,
	})
	return patch
}

func createPatch(availableKeys map[string]string, values map[string]string) []patchOperation {
	var patch []patchOperation

	if _, ok := values["nci.yunshan.net/ips"]; ok {
		patch = append(patch, updateAnnotations(availableKeys, values)...)
	} else {
		patch = append(patch, updateLabels(availableKeys, values)...)
	}
	return patch
}

// main mutation process
func (whsvr *WebhookServer) mutate(ar *v1.AdmissionReview) *v1.AdmissionResponse {
	req := ar.Request
	var svmate serverMate
	svmate.vpcprefix, svmate.abnormalws, svmate.op = whsvr.vpcprefix, whsvr.abnormalws, req.Operation

	// 实例化 DynamicClient
	config, err := clientcmd.BuildConfigFromFlags("", "")
	if err != nil {
		panic(err.Error())
	}

	svmate.client.dynamicClient, err = dynamic.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	glog.Infof("AdmissionReview for Kind=%v, Name=%v UID=%v patchOperation=%v UserInfo=%v",
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
		glog.Infof("start mutateNamespce")
		return mutateNamespce(svmate, &namespace)
	case "Deployment":
		var deployment appsv1.Deployment
		if err := json.Unmarshal(req.Object.Raw, &deployment); err != nil {
			glog.Errorf("Could not unmarshal raw object: %v", err)
			return &v1.AdmissionResponse{
				Result: &metav1.Status{
					Message: err.Error(),
				},
			}
		}
		glog.Infof("start mutateDeploy")
		return mutateDeploy(svmate, &deployment)
	case "Workspace":
		glog.Infof("start vpcHandler")
		return vpcHandler(req.Name, svmate)
	default:
		msg := fmt.Sprintf("\nNot support for this Kind of resource  %v", req.Kind.Kind)
		glog.Infof(msg)
		return &v1.AdmissionResponse{
			Result: &metav1.Status{
				Message: msg,
			},
		}
	}

}

// Serve method for webhook server
func (whsvr *WebhookServer) serve(w http.ResponseWriter, r *http.Request) {
	var body []byte

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

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
		glog.Errorf("Can't decode body: %v", err)
		admissionResponse = &v1.AdmissionResponse{
			Result: &metav1.Status{
				Message: err.Error(),
			},
		}
	} else {
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

func (c *Client) workspaceExist(workspaceName string) bool {

	// 设置要请求的 GVR
	gvr := schema.GroupVersionResource{
		Group:    "tenant.kubesphere.io",
		Version:  "v1alpha1",
		Resource: "workspaces",
	}

	// 发送请求，并得到返回结果
	unStructData, err := c.dynamicClient.Resource(gvr).Get(context.TODO(), workspaceName, metav1.GetOptions{})
	if err != nil {
		glog.Error(err.Error())
		return false
	}

	var obj v1alpha1.Workspace

	// 使用 runtime.DefaultUnstructuredConverter 转换 item 为对象
	err = runtime.DefaultUnstructuredConverter.FromUnstructured(unStructData.UnstructuredContent(), &obj)
	if err != nil {
		glog.Error(err.Error())
		//return false

	}

	return true
}
