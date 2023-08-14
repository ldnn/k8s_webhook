package main

import (
	"context"
	"fmt"

	"github.com/golang/glog"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func addEphemeralStorage(containers []corev1.Container) []patchOperation {

	ephemeralStorageMax := resource.MustParse("2Gi")
	var patches []patchOperation

	for i := range containers {
		// 判断容器的 ephemeral-storage 资源配置是否存在并小于2G
		ephemeralStorage, ok := containers[i].Resources.Limits[corev1.ResourceEphemeralStorage]
		if ok {
			if !ephemeralStorage.IsZero() && ephemeralStorage.Cmp(ephemeralStorageMax) <= 0 {
				continue
			}
		}
		// 修改容器的 ephemeral-storage 资源配置
		patches = append(patches, patchOperation{
			Op:    "add",
			Path:  fmt.Sprintf("/spec/template/spec/containers/%d/resources/requests/%s", i, corev1.ResourceEphemeralStorage),
			Value: ephemeralStorageMax,
		})
		patches = append(patches, patchOperation{
			Op:    "add",
			Path:  fmt.Sprintf("/spec/template/spec/containers/%d/resources/limits/%s", i, corev1.ResourceEphemeralStorage),
			Value: ephemeralStorageMax,
		})
	}
	return patches
}

func (c *Client) chekWorkspace(namespace string) bool {

	// 设置要请求的 GVR
	gvr := schema.GroupVersionResource{
		Group:    "",
		Version:  "v1",
		Resource: "namespaces",
	}

	// 发送请求，并得到返回结果
	unStructData, err := c.dynamicClient.Resource(gvr).Get(context.TODO(), namespace, metav1.GetOptions{})
	if err != nil {
		glog.Error(err.Error())
		return false
	}

	var obj corev1.Namespace

	// 使用 runtime.DefaultUnstructuredConverter 转换 item 为对象
	err = runtime.DefaultUnstructuredConverter.FromUnstructured(unStructData.UnstructuredContent(), &obj)
	if err != nil {
		glog.Error(err.Error())
		//return false

	}

	workspace, ok := obj.GetLabels()["kubesphere.io/workspace"]
	if ok && workspace == "system-workspace" {
		return true
	}

	return true
}
