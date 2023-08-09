package main

import (
	"encoding/json"
	"fmt"

	"github.com/golang/glog"
	"gomodules.xyz/jsonpatch/v2"
	v1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func mutatePod(pod *corev1.Pod) *v1.AdmissionResponse {
	patchesBytes, err := addEphemeralStorage(pod)

	if err != nil {
		return &v1.AdmissionResponse{
			Result: &metav1.Status{
				Message: err.Error(),
			},
		}
	}

	glog.Infof("AdmissionResponse: patch=%v\n", string(patchesBytes))

	return &v1.AdmissionResponse{
		Allowed: true,
		Patch:   patchesBytes,
		PatchType: func() *v1.PatchType {
			pt := v1.PatchTypeJSONPatch
			return &pt
		}(),
	}
}

func addEphemeralStorage(pod *corev1.Pod) ([]byte, error) {

	ephemeralStorageMax := resource.MustParse("2Gi")
	var patches []jsonpatch.JsonPatchOperation

	for i := range pod.Spec.Containers {
		// 判断容器的 ephemeral-storage 资源配置是否存在并小于2G
		ephemeralStorage, ok := pod.Spec.Containers[i].Resources.Limits[corev1.ResourceEphemeralStorage]
		if ok {
			if !ephemeralStorage.IsZero() && ephemeralStorage.Cmp(ephemeralStorageMax) <= 0 {
				continue
			}
		}
		// 修改容器的 ephemeral-storage 资源配置
		patches = append(patches, jsonpatch.JsonPatchOperation{
			Operation: "add",
			Path:      fmt.Sprintf("/spec/containers/%d/resources/requests/%s", i, corev1.ResourceEphemeralStorage),
			Value:     ephemeralStorageMax,
		})
		patches = append(patches, jsonpatch.JsonPatchOperation{
			Operation: "add",
			Path:      fmt.Sprintf("/spec/containers/%d/resources/limits/%s", i, corev1.ResourceEphemeralStorage),
			Value:     ephemeralStorageMax,
		})
	}

	return json.Marshal(patches)

}
